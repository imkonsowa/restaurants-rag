# Introduction

A playground for building a simple AI agent using golang. The application is a restaurant search interface that finds
restaurants by free text search.

**(Note)**: This is not meant to be a 100% microservices app, I have simplified a lot of things like having a shared
config between all running containers, not having a separate database for each service, etc. This is just to learn how
to build a simple AI agent using golang.

# Pre-requisites

- Docker
- Docker Compose
- Make

# Tech Stack

- Golang
- Postgres (PgVector, PostGIS, and wal2json with Logical Replication)
- NATS
- Javascript

# Run the code

- Clone the repository
- Run the following command to build docker images and run the application using docker-compose:

```bash
make run
```

- Add restaurants to the database from `restaurants_sample.json` file using the following command:

```bash
make add-restaurants
```

- Access the application at http://localhost:8000

## Directory Structure

```
├── agent: retrieval logic and the web interface 
├── cdc: captures data changes and publish to NATS
├── embedder: listens to NATS and embeds the restaurant data
├── platform
    ├── docker: contains the docker-compose file and images docker files
    ├── sql: database init sql scripts
```
