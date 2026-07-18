DB_URL      ?= postgres://postgres:123456@localhost:5432/omnigen?sslmode=disable
TEST_DB_URL ?= postgres://postgres:123456@localhost:5432/omnigen_test?sslmode=disable

# go install 把工具装到 GOPATH/bin，该目录通常不在 PATH 上（本机已确认不在），
# 因此一律用绝对路径调用，避免 "command not found"。
GOBIN   := $(shell go env GOPATH)/bin
MIGRATE := $(GOBIN)/migrate
WIRE    := $(GOBIN)/wire

.PHONY: migrate-up migrate-down migrate-test-up test wire run tools

tools:
	go install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	go install github.com/google/wire/cmd/wire@latest

migrate-up:
	$(MIGRATE) -path migrations -database "$(DB_URL)" up

migrate-down:
	$(MIGRATE) -path migrations -database "$(DB_URL)" down 1

migrate-test-up:
	$(MIGRATE) -path migrations -database "$(TEST_DB_URL)" up

test: migrate-test-up
	go test ./... -v

wire:
	$(WIRE) ./internal/...

run:
	go run ./cmd/server
