SHELL := /bin/zsh

GO ?= go
APP_ADDR ?= :8081
DB_PATH ?= data
DB_NAME ?= timeleak.db

.PHONY: deps run test clean

deps:
	$(GO) mod tidy

run: deps
	@mkdir -p $(DB_PATH)
	APP_ADDR=$(APP_ADDR) DB_PATH=$(DB_PATH) DB_NAME=$(DB_NAME) $(GO) run ./cmd

test: deps
	$(GO) test -v ./...

clean:
	rm -rf data
