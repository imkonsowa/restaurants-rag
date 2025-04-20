# Introduction

A playground for building a simple AI agent using golang. The app is a restaurant search interface that finds
restaurants by free text search.

**(Note)**:  This is not meant for production, it's a learning playground.

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
- Run this command to build and run the application:

```bash
make run
```

- Add restaurants to the database from `restaurants_sample.json`:

```bash
make add-restaurants
```

- Access the application at http://localhost:8000

# Search Restaurant

Test the following queries in the search input:

- `find me a nearby sushi restaurant`
- `find me a nearby kabab restaurant`


## Directory Structure

```
├── agent: retrieval logic and the web interface 
├── cdc: captures data changes and publish to NATS
├── embedder: listens to NATS and embeds the restaurant data
├── platform
    ├── docker: contains the docker-compose file and images docker files
    ├── sql: database init sql scripts
```
