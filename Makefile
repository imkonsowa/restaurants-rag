pg-image:
	docker build -f platform/docker/pg.Dockerfile -t imkonsowa/postgis-vector-wal2json:latest .

agent-image:
	docker build -f platform/docker/agent.Dockerfile -t imkonsowa/agent:latest .

cdc-image:
	docker build -f platform/docker/cdc.Dockerfile -t imkonsowa/cdc:latest .

embedder-image:
	docker build -f platform/docker/embedder.Dockerfile -t imkonsowa/embedder:latest .

images: pg-image agent-image cdc-image embedder-image

up:
	docker-compose -p rag -f platform/docker/docker-compose.yaml up -d

down:
	docker compose -p rag -f platform/docker/docker-compose.yaml down -v

run:
	make -j4 images
	make up

add-restaurants:
	curl -X POST -H "Content-Type: application/json" -d @restaurants_sample.json http://localhost:8080/restaurants
