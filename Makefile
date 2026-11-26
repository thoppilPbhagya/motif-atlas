.PHONY: up down migrate

## Start Postgres and Redis and wait until both are healthy
up:
	docker compose up -d --wait

## Stop the containers (data is kept in the volumes)
down:
	docker compose down

## Apply new SQL files in db/migrations, in order
migrate:
	docker compose up -d --wait postgres
	bash scripts/migrate.sh