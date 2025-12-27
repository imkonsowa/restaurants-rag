package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/imkonsowa/restaurants-rag/config"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

const outputPlugin = "wal2json"

type WAL2JSONMessage struct {
	Change []WAL2JSONChange `json:"change"`
}

type WAL2JSONChange struct {
	Kind         string        `json:"kind"`
	Table        string        `json:"table"`
	ColumnNames  []string      `json:"columnnames,omitempty"`
	ColumnValues []interface{} `json:"columnvalues,omitempty"`
}

type Listener struct {
	config *config.Config
	nats   *NatsClient

	regularConn   *pgx.Conn
	replConn      *pgconn.PgConn
	clientXLogPos pglogrepl.LSN
}

func NewListener(cfg *config.Config, nc *NatsClient) *Listener {
	return &Listener{
		config: cfg,
		nats:   nc,
	}
}

func (l *Listener) Run(ctx context.Context) error {
	slog.Info("starting WAL listener")

	var err error
	l.regularConn, err = pgx.Connect(ctx, l.config.Postgres.ConnStr())
	if err != nil {
		return fmt.Errorf("connect to postgres: %w", err)
	}

	if err := l.ensurePublication(ctx); err != nil {
		return err
	}

	slotExists, err := l.slotExists(ctx)
	if err != nil {
		return fmt.Errorf("check replication slot: %w", err)
	}

	l.replConn, err = pgconn.Connect(ctx, l.config.Postgres.ReplicationConnStr())
	if err != nil {
		return fmt.Errorf("connect for replication: %w", err)
	}

	sysident, err := pglogrepl.IdentifySystem(ctx, l.replConn)
	if err != nil {
		return fmt.Errorf("identify system: %w", err)
	}

	startLSN, err := l.resolveStartLSN(ctx, slotExists, sysident.XLogPos)
	if err != nil {
		return err
	}

	err = pglogrepl.StartReplication(ctx, l.replConn, l.config.Replication.Slot, startLSN,
		pglogrepl.StartReplicationOptions{
			PluginArgs: []string{
				"\"pretty-print\" 'false'",
				"\"include-xids\" 'false'",
				"\"include-timestamp\" 'false'",
				"\"include-lsn\" 'false'",
			},
		},
	)
	if err != nil {
		return fmt.Errorf("start replication: %w", err)
	}

	slog.Info("replication started", "slot", l.config.Replication.Slot, "lsn", startLSN)

	l.clientXLogPos = startLSN
	return l.listen(ctx)
}

func (l *Listener) listen(ctx context.Context) error {
	standbyTimeout := 10 * time.Second
	nextStandbyDeadline := time.Now().Add(standbyTimeout)

	for {
		if time.Now().After(nextStandbyDeadline) {
			if err := l.sendStandbyStatus(ctx); err != nil {
				return err
			}
			nextStandbyDeadline = time.Now().Add(standbyTimeout)
		}

		receiveCtx, cancel := context.WithDeadline(ctx, nextStandbyDeadline)
		rawMsg, err := l.replConn.ReceiveMessage(receiveCtx)
		cancel()

		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}
			return fmt.Errorf("receive message: %w", err)
		}

		if errMsg, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			return fmt.Errorf("postgres WAL error: %+v", errMsg)
		}

		msg, ok := rawMsg.(*pgproto3.CopyData)
		if !ok {
			continue
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("parse keepalive: %w", err)
			}
			if pkm.ServerWALEnd > l.clientXLogPos {
				l.clientXLogPos = pkm.ServerWALEnd
			}
			if pkm.ReplyRequested {
				nextStandbyDeadline = time.Time{}
			}

		case pglogrepl.XLogDataByteID:
			xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("parse xlog: %w", err)
			}

			if len(xld.WALData) > 0 {
				var walMsg WAL2JSONMessage
				if err := json.Unmarshal(xld.WALData, &walMsg); err != nil {
					slog.Error("parse wal2json", "err", err)
					continue
				}
				l.processChanges(walMsg.Change)
			}

			if xld.WALStart > l.clientXLogPos {
				l.clientXLogPos = xld.WALStart
			}
		}
	}
}

func (l *Listener) processChanges(changes []WAL2JSONChange) {
	tableSubjects := map[string]string{
		"restaurants": l.config.Nats.RestaurantsSubject,
		"menu_items":  l.config.Nats.MenuItemsSubject,
		"categories":  l.config.Nats.CategoriesSubject,
	}

	for _, change := range changes {
		if change.Kind != "insert" && change.Kind != "update" {
			continue
		}

		if change.Kind == "update" && hasEmbedding(change) {
			continue
		}

		subject, ok := tableSubjects[change.Table]
		if !ok {
			continue
		}

		id := extractID(change)
		if id == 0 {
			continue
		}

		data, _ := json.Marshal(map[string]interface{}{
			"table": change.Table,
			"kind":  change.Kind,
			"id":    id,
		})

		if err := l.nats.Publish(subject, data); err != nil {
			slog.Error("publish to nats", "err", err, "subject", subject)
		}
	}
}

func (l *Listener) Close(ctx context.Context) {
	if l.regularConn != nil {
		l.regularConn.Close(ctx)
	}
	if l.replConn != nil {
		l.replConn.Close(ctx)
	}
}

func (l *Listener) ensurePublication(ctx context.Context) error {
	var exists bool
	err := l.regularConn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)",
		l.config.Replication.Name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("check publication: %w", err)
	}

	if !exists {
		_, err = l.regularConn.Exec(ctx,
			fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES", l.config.Replication.Name))
		if err != nil {
			return fmt.Errorf("create publication: %w", err)
		}
		slog.Info("created publication", "name", l.config.Replication.Name)
	}
	return nil
}

func (l *Listener) slotExists(ctx context.Context) (bool, error) {
	var exists bool
	err := l.regularConn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)",
		l.config.Replication.Slot).Scan(&exists)
	return exists, err
}

func (l *Listener) getSlotLSN(ctx context.Context) (pglogrepl.LSN, error) {
	var lsnStr *string
	err := l.regularConn.QueryRow(ctx,
		"SELECT confirmed_flush_lsn FROM pg_replication_slots WHERE slot_name = $1",
		l.config.Replication.Slot).Scan(&lsnStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("slot %s does not exist", l.config.Replication.Slot)
		}
		return 0, err
	}
	if lsnStr == nil {
		return 0, nil
	}
	return pglogrepl.ParseLSN(*lsnStr)
}

func (l *Listener) resolveStartLSN(ctx context.Context, slotExists bool, sysLSN pglogrepl.LSN) (pglogrepl.LSN, error) {
	if slotExists {
		lsn, err := l.getSlotLSN(ctx)
		if err != nil || lsn == 0 {
			return sysLSN, nil
		}
		return lsn, nil
	}

	result, err := pglogrepl.CreateReplicationSlot(ctx, l.replConn, l.config.Replication.Slot, outputPlugin,
		pglogrepl.CreateReplicationSlotOptions{Temporary: false})
	if err != nil {
		return 0, fmt.Errorf("create replication slot: %w", err)
	}

	slog.Info("created replication slot", "name", l.config.Replication.Slot)
	return pglogrepl.ParseLSN(result.ConsistentPoint)
}

func (l *Listener) sendStandbyStatus(ctx context.Context) error {
	return pglogrepl.SendStandbyStatusUpdate(ctx, l.replConn, pglogrepl.StandbyStatusUpdate{
		WALWritePosition: l.clientXLogPos,
	})
}

func hasEmbedding(change WAL2JSONChange) bool {
	for i, name := range change.ColumnNames {
		if name == "embedding" && i < len(change.ColumnValues) {
			return change.ColumnValues[i] != nil
		}
	}
	return false
}

func extractID(change WAL2JSONChange) uint64 {
	for i, name := range change.ColumnNames {
		if name == "id" && i < len(change.ColumnValues) {
			if v, ok := change.ColumnValues[i].(float64); ok {
				return uint64(v)
			}
		}
	}
	return 0
}
