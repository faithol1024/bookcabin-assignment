-include .env
export

COMPOSE := docker compose

.PHONY: run test lint bench secrets hooks docker-up docker-down

run:
	go run ./cmd/app-http

test:
	go test -race -cover ./...

lint:
	golangci-lint run

bench:
	go test -run='^$$' -bench=. -benchmem ./...

secrets:
	gitleaks git --redact --no-banner

hooks:
	git config core.hooksPath .githooks

docker-up:
	$(COMPOSE) up -d --build

docker-down:
	$(COMPOSE) down
