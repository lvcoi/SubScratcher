SHELL := /bin/bash

GO ?= go
BIN_DIR ?= bin

ROOT := $(CURDIR)
INSPECT_DIR := Inspect
KNOCK_DIR := Knock
SCRATCH_DIR := Scratch
TESTENV_DIR := testenv

INSPECT_BIN := $(BIN_DIR)/inspect
KNOCK_BIN := $(BIN_DIR)/knock
SCRATCH_BIN := $(BIN_DIR)/scratch
MOCKENV_BIN := $(BIN_DIR)/mockenv

MOCK_BIND ?= 127.0.0.1
MOCK_HTTP ?= 8080
MOCK_HTTPS ?= 8443
MOCK_RAW ?= 5666
ALLOW_HOST ?= allowed.test
SCRATCH_DOMAIN ?= local.test

.PHONY: help build build-inspect build-knock build-scratch build-mockenv \
	clean run-mockenv run-knock run-inspect run-scratch inspect-pipe scratch-offline

help:
	@printf "Targets:\n"
	@printf "  build              Build all binaries into $(BIN_DIR)/\n"
	@printf "  run-mockenv        Start mock HTTP/HTTPS/RAW services\n"
	@printf "  run-knock          Run Knock against localhost\n"
	@printf "  inspect-pipe       Pipe Knock -> Inspect using allowed Host\n"
	@printf "  scratch-offline    Run Scratch with local hosts + offline mode\n"

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

build: build-inspect build-knock build-scratch build-mockenv

build-inspect: | $(BIN_DIR)
	$(GO) -C $(INSPECT_DIR) build -o $(ROOT)/$(INSPECT_BIN) ./cmd

build-knock: | $(BIN_DIR)
	$(GO) -C $(KNOCK_DIR) build -o $(ROOT)/$(KNOCK_BIN) ./cmd

build-scratch: | $(BIN_DIR)
	$(GO) -C $(SCRATCH_DIR) build -o $(ROOT)/$(SCRATCH_BIN) ./cmd

build-mockenv: | $(BIN_DIR)
	$(GO) -C $(TESTENV_DIR) build -o $(ROOT)/$(MOCKENV_BIN) ./cmd/mockenv

run-mockenv: build-mockenv
	$(MOCKENV_BIN) -bind $(MOCK_BIND) -http $(MOCK_HTTP) -https $(MOCK_HTTPS) -raw $(MOCK_RAW) -allow $(ALLOW_HOST)

run-knock: build-knock
	$(KNOCK_BIN) -t 127.0.0.1 -desc

inspect-pipe: build-knock build-inspect
	$(KNOCK_BIN) -t 127.0.0.1 -s -d $(ALLOW_HOST) | $(INSPECT_BIN)

run-scratch: build-scratch
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist.txt

scratch-offline: build-scratch
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist.txt -hosts $(TESTENV_DIR)/hosts.txt -offline -url

clean:
	@rm -rf $(BIN_DIR)
