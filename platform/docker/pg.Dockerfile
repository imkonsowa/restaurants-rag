FROM postgis/postgis:17-3.4

RUN apt-get update \
    && apt-get install -y postgresql-17-wal2json postgresql-17-pgvector \
    && rm -rf /var/lib/apt/lists/*

RUN echo "wal_level = logical" >> /usr/share/postgresql/postgresql.conf.sample
