.PHONY: up down logs seed api web build-web test import-har preview-prices update-prices

HAR_INPUT ?= data/har

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

import-har:
	python3 scripts/import_har_prices.py --input $(HAR_INPUT)

preview-prices:
	python3 scripts/import_har_prices.py --input $(HAR_INPUT) --preview-listings-diff

update-prices:
	python3 scripts/import_har_prices.py --input $(HAR_INPUT) --apply-to-listings
	cd backend && go run ./cmd/seed -validate-only
