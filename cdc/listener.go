package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"time"

	"github.com/imkonsowa/restaurants-rag/config"

	"github.com/jackc/pglogrepl"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

const (
	outputPlugin = "wal2json"
)

// WAL2JSONMessage represents the top-level structure of a wal2json message
type WAL2JSONMessage struct {
	XID       int64            `json:"xid,omitempty"`
	Timestamp string           `json:"timestamp,omitempty"`
	LSN       string           `json:"lsn,omitempty"`
	NextLSN   string           `json:"nextlsn,omitempty"`
	Change    []WAL2JSONChange `json:"change"`
}

// WAL2JSONChange represents a single change in a wal2json message
type WAL2JSONChange struct {
	Kind         string           `json:"kind"`
	Schema       string           `json:"schema"`
	Table        string           `json:"table"`
	ColumnNames  []string         `json:"columnnames,omitempty"`
	ColumnTypes  []string         `json:"columntypes,omitempty"`
	ColumnValues []interface{}    `json:"columnvalues,omitempty"`
	OldKeys      *WAL2JSONOldKeys `json:"oldkeys,omitempty"`
}

// WAL2JSONOldKeys represents the keys of a row before an update or delete
type WAL2JSONOldKeys struct {
	KeyNames  []string      `json:"keynames"`
	KeyTypes  []string      `json:"keytypes"`
	KeyValues []interface{} `json:"keyvalues"`
}

// Listener is a struct that handles PostgreSQL WAL replication
type Listener struct {
	config *config.Config
	nats   *NatsClient

	regularConn   *pgx.Conn
	replConn      *pgconn.PgConn
	clientXLogPos pglogrepl.LSN
}

// NewListener creates a new Listener instance
func NewListener(cfg *config.Config, nc *NatsClient) *Listener {
	return &Listener{
		config: cfg,
		nats:   nc,
	}

}

// Run starts the WAL listening process
func (l *Listener) Run(ctx context.Context) error {
	log.Println("Starting WAL reader with wal2json using PostgreSQL replication slots...")

	// Create a regular connection for standard queries
	var err error
	l.regularConn, err = pgx.Connect(ctx, l.config.Postgres.ConnStr())
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL server for standard queries: %w", err)
	}

	var exists bool
	err = l.regularConn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_publication WHERE pubname = $1)", l.config.Replication.Name).Scan(&exists)
	if err != nil {
		return fmt.Errorf("error checking if publication exists: %w", err)
	}

	if !exists {
		_, err = l.regularConn.Exec(ctx, fmt.Sprintf("CREATE PUBLICATION %s FOR ALL TABLES;", l.config.Replication.Name))
		if err != nil {
			return fmt.Errorf("create publication error: %w", err)
		}
		log.Println("Created publication: pglogrepl_demo")
	}

	// Check if slot exists
	slotExists, err := l.checkSlotExists(ctx)
	if err != nil {
		return fmt.Errorf("error checking replication slot: %w", err)
	}

	// Create a replication connection
	l.replConn, err = pgconn.Connect(ctx, l.config.Postgres.ReplicationConnStr())
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL server for replication: %w", err)
	}

	// Identify system
	sysident, err := pglogrepl.IdentifySystem(ctx, l.replConn)
	if err != nil {
		return fmt.Errorf("IdentifySystem failed: %w", err)
	}
	log.Println("SystemID:", sysident.SystemID, "Timeline:", sysident.Timeline, "XLogPos:", sysident.XLogPos, "DBName:", sysident.DBName)

	var startLSN pglogrepl.LSN
	if slotExists {
		// Get the current position from the slot
		startLSN, err = l.getSlotLSN(ctx)
		if err != nil {
			log.Println("Warning: Failed to get LSN from slot:", err)
			startLSN = sysident.XLogPos
		} else if startLSN == 0 {
			// Slot exists but has never been used
			startLSN = sysident.XLogPos
		}
		log.Printf("Using existing replication slot: %s at position %s", l.config.Replication.Slot, startLSN)
	} else {
		// Create a new replication slot
		slotResult, err := pglogrepl.CreateReplicationSlot(
			ctx,
			l.replConn,
			l.config.Replication.Slot,
			outputPlugin,
			pglogrepl.CreateReplicationSlotOptions{
				Temporary: false,
			},
		)
		if err != nil {
			return fmt.Errorf("CreateReplicationSlot failed: %w", err)
		}

		// Convert the ConsistentPoint string to pglogrepl.LSN
		startLSN, err = pglogrepl.ParseLSN(slotResult.ConsistentPoint)
		if err != nil {
			return fmt.Errorf("failed to parse LSN from slot creation: %w", err)
		}

		log.Printf("Created replication slot: %s at position %s", l.config.Replication.Slot, startLSN)
	}

	// Set up wal2json plugin arguments
	pluginArgs := []string{
		"\"pretty-print\" 'true'",
		"\"include-xids\" 'true'",
		"\"include-timestamp\" 'true'",
		"\"include-lsn\" 'true'",
	}

	// Start replication
	err = pglogrepl.StartReplication(
		ctx,
		l.replConn,
		l.config.Replication.Slot,
		startLSN,
		pglogrepl.StartReplicationOptions{
			PluginArgs: pluginArgs,
		},
	)
	if err != nil {
		return fmt.Errorf("StartReplication failed: %w", err)
	}
	log.Printf("Logical replication started on slot %s from LSN %s", l.config.Replication.Slot, startLSN)

	// Set up standby message timing
	l.clientXLogPos = startLSN
	standbyMessageTimeout := time.Second * 10
	nextStandbyMessageDeadline := time.Now().Add(standbyMessageTimeout)

	// Main replication loop
	for {
		// Send standby status update if needed
		if time.Now().After(nextStandbyMessageDeadline) {
			// This updates the replication slot's confirmed_flush_lsn
			err = pglogrepl.SendStandbyStatusUpdate(ctx, l.replConn, pglogrepl.StandbyStatusUpdate{
				WALWritePosition: l.clientXLogPos,
			})
			if err != nil {
				return fmt.Errorf("SendStandbyStatusUpdate failed: %w", err)
			}
			log.Printf("Sent standby status update at %s", l.clientXLogPos.String())
			nextStandbyMessageDeadline = time.Now().Add(standbyMessageTimeout)
		}

		// Receive message with timeout
		receiveCtx, cancel := context.WithDeadline(ctx, nextStandbyMessageDeadline)
		rawMsg, err := l.replConn.ReceiveMessage(receiveCtx)
		cancel()
		if err != nil {
			if pgconn.Timeout(err) {
				continue
			}

			return fmt.Errorf("ReceiveMessage failed: %w", err)
		}

		// Handle error response
		if errMsg, ok := rawMsg.(*pgproto3.ErrorResponse); ok {
			return fmt.Errorf("received Postgres WAL error: %+v", errMsg)
		}

		// Handle copy data
		msg, ok := rawMsg.(*pgproto3.CopyData)
		if !ok {
			log.Printf("Received unexpected message: %T", rawMsg)

			continue
		}

		switch msg.Data[0] {
		case pglogrepl.PrimaryKeepaliveMessageByteID:
			// Process keepalive message
			pkm, err := pglogrepl.ParsePrimaryKeepaliveMessage(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("ParsePrimaryKeepaliveMessage failed: %w", err)
			}

			log.Println("Primary Keepalive Message =>", "ServerWALEnd:", pkm.ServerWALEnd, "ServerTime:", pkm.ServerTime, "ReplyRequested:", pkm.ReplyRequested)

			if pkm.ServerWALEnd > l.clientXLogPos {
				l.clientXLogPos = pkm.ServerWALEnd
			}

			if pkm.ReplyRequested {
				nextStandbyMessageDeadline = time.Time{}
			}

		case pglogrepl.XLogDataByteID:
			// Process WAL data
			xld, err := pglogrepl.ParseXLogData(msg.Data[1:])
			if err != nil {
				return fmt.Errorf("ParseXLogData failed: %w", err)
			}

			// Process wal2json data
			walData := xld.WALData
			if len(walData) == 0 {
				continue
			}

			walMessage, err := l.parseWal2JsonMessage(walData)
			if err != nil {
				slog.Error("Error parsing wal2json data", "err", err, "data", string(walData))
				continue
			}

			l.processWal2JsonData(walMessage)

			// Update clientXLogPos to the WAL end position
			if xld.WALStart > l.clientXLogPos {
				l.clientXLogPos = xld.WALStart
			}

			// For significant changes, update standby status immediately
			if len(walMessage.Change) > 0 {
				err = pglogrepl.SendStandbyStatusUpdate(ctx, l.replConn, pglogrepl.StandbyStatusUpdate{
					WALWritePosition: l.clientXLogPos,
				})
				if err != nil {
					log.Println("Warning: Failed to send immediate standby status update:", err)
				} else {
					log.Printf("Sent immediate standby status update at %s after processing changes", l.clientXLogPos)
				}
			}
		}
	}
}

// Close closes all connections
func (l *Listener) Close(ctx context.Context) {
	if l.regularConn != nil {
		l.regularConn.Close(ctx)
	}
	if l.replConn != nil {
		l.replConn.Close(ctx)
	}
}

func (l *Listener) processWal2JsonData(walMessage *WAL2JSONMessage) {
	// Log LSN
	if walMessage.NextLSN != "" {
		log.Printf("Processing WAL data with nextlsn: %s", walMessage.NextLSN)
	}

	// Log timestamp
	if walMessage.Timestamp != "" {
		log.Printf("Event timestamp: %s", walMessage.Timestamp)
	}

	// Process changes
	for _, change := range walMessage.Change {
		// Create a map to hold column data for easier access
		columnData := make(map[string]interface{})
		for i, name := range change.ColumnNames {
			if i < len(change.ColumnValues) {
				columnData[name] = change.ColumnValues[i]
			}
		}

		// Process by change type
		switch change.Kind {
		case "insert", "update":
			tableSubjectMap := map[string]string{
				"restaurants": l.config.Nats.RestaurantsSubject,
				"menu_items":  l.config.Nats.MenuItemsSubject,
				"categories":  l.config.Nats.CategoriesSubject,
			}
			subject, ok := tableSubjectMap[change.Table]
			if !ok {
				return
			}

			message := map[string]interface{}{
				"table": change.Table,
				"kind":  change.Kind,
				"id":    uint64(columnData["id"].(float64)),
			}

			data, err := json.Marshal(message)
			if err != nil {
				log.Println("Error marshalling data to JSON:", err)
				return
			}

			err = l.nats.Publish(subject, data)
			if err != nil {
				log.Println("Error publishing data to NATS:", err)
			}
		}
	}
}

func (l *Listener) parseWal2JsonMessage(data []byte) (*WAL2JSONMessage, error) {
	var result WAL2JSONMessage
	err := json.Unmarshal(data, &result)
	if err != nil {
		return nil, fmt.Errorf("failed to parse wal2json data: %w", err)
	}
	return &result, nil
}

// getSlotLSN retrieves the confirmed_flush_lsn of a replication slot
func (l *Listener) getSlotLSN(ctx context.Context) (pglogrepl.LSN, error) {
	var lsnStr string
	err := l.regularConn.QueryRow(ctx, "SELECT confirmed_flush_lsn FROM pg_replication_slots WHERE slot_name = $1", l.config.Replication.Slot).Scan(&lsnStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, fmt.Errorf("replication slot %s does not exist", l.config.Replication.Slot)
		}
		return 0, fmt.Errorf("error querying replication slot: %w", err)
	}

	if lsnStr == "" {
		// Slot exists but confirmed_flush_lsn is null (slot hasn't been used yet)
		return 0, nil
	}

	return pglogrepl.ParseLSN(lsnStr)
}

// checkSlotExists checks if a replication slot exists
func (l *Listener) checkSlotExists(ctx context.Context) (bool, error) {
	var exists bool
	err := l.regularConn.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_replication_slots WHERE slot_name = $1)", l.config.Replication.Slot).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("error checking if slot exists: %w", err)
	}

	return exists, nil
}
