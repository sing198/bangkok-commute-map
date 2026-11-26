.PHONY: up down logs seed api web build-web test import-har preview-prices update-prices sync-portal-prices sync-airbnb import-kaggle update-kaggle-prices

HAR_INPUT ?= data/har
KAGGLE_INPUT ?= data/kaggle

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

sync-portal-prices:
	python3 scripts/sync_thai_portal_prices.py
	cd backend && go test ./cmd/seed ./internal/commute

sync-airbnb:
	python3 scripts/fetch_inside_airbnb.py
	cd backend && go test ./cmd/seed ./internal/commute

import-kaggle:
	python3 scripts/import_kaggle_prices.py --input $(KAGGLE_INPUT) --preview-listings-diff

update-kaggle-prices:
	python3 scripts/import_kaggle_prices.py --input $(KAGGLE_INPUT) --apply-to-listings
	cd backend && go run ./cmd/seed -validate-only
