.PHONY: up down logs seed api web build-web test

up:
	docker compose up -d

down:
	docker compose down

logs:
	docker compose logs -f

seed:
	cd backend && go run ./cmd/seed

api:
	cd backend && go run ./cmd/api

web:
	cd frontend && npm run dev

build-web:
	cd frontend && npm run build

test:
	cd backend && go test -v ./...
