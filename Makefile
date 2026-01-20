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
	clean run-mockenv run-knock run-inspect run-scratch inspect-pipe scratch-offline scratch-knock-ip scratch-knock-inspect test-all

help:
	@printf "Targets:\n"
	@printf "  build              Build all binaries into $(BIN_DIR)/\n"
	@printf "  run-mockenv        Start mock HTTP/HTTPS/RAW services\n"
	@printf "  run-knock          Run Knock against localhost\n"
	@printf "  inspect-pipe       Pipe Knock -> Inspect using allowed Host\n"
	@printf "  scratch-offline    Run Scratch with local hosts + offline mode\n"
	@printf "  scratch-knock-ip   Test Scratch -ip -> Knock stdin pipeline\n"
	@printf "  scratch-knock-inspect  Test Scratch -> Knock -> Inspect pipeline\n"
	@printf "  test-all           Run Knock, Inspect, and Scratch against mockenv\n"

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
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist.txt -hosts $(TESTENV_DIR)/hosts.txt -offline -ip

scratch-knock-ip: build-scratch build-knock build-mockenv
	@set -euo pipefail; \
	$(MOCKENV_BIN) -bind $(MOCK_BIND) -http $(MOCK_HTTP) -https $(MOCK_HTTPS) -raw $(MOCK_RAW) -allow $(ALLOW_HOST) & \
	mock_pid=$$!; \
	trap 'kill $$mock_pid 2>/dev/null || true' EXIT; \
	sleep 0.5; \
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist-local.txt -hosts $(TESTENV_DIR)/hosts-local.txt -offline -ip | \
		LC_ALL=C sort -u | \
		$(KNOCK_BIN) -desc; \
	kill $$mock_pid 2>/dev/null || true; \
	wait $$mock_pid 2>/dev/null || true

scratch-knock-inspect: build-scratch build-knock build-inspect build-mockenv
	@set -euo pipefail; \
	$(MOCKENV_BIN) -bind $(MOCK_BIND) -http $(MOCK_HTTP) -https $(MOCK_HTTPS) -raw $(MOCK_RAW) -allow $(ALLOW_HOST) & \
	mock_pid=$$!; \
	trap 'kill $$mock_pid 2>/dev/null || true' EXIT; \
	sleep 0.5; \
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist-local.txt -hosts $(TESTENV_DIR)/hosts-local.txt -offline -ip | \
		LC_ALL=C sort -u | \
		$(KNOCK_BIN) -s -d $(ALLOW_HOST) | \
		$(INSPECT_BIN); \
	kill $$mock_pid 2>/dev/null || true; \
	wait $$mock_pid 2>/dev/null || true

test-all: build build-mockenv
	@set -euo pipefail; \
	$(MOCKENV_BIN) -bind $(MOCK_BIND) -http $(MOCK_HTTP) -https $(MOCK_HTTPS) -raw $(MOCK_RAW) -allow $(ALLOW_HOST) & \
	mock_pid=$$!; \
	trap 'kill $$mock_pid 2>/dev/null || true' EXIT; \
	sleep 0.5; \
	$(KNOCK_BIN) -t 127.0.0.1 -desc; \
	$(KNOCK_BIN) -t 127.0.0.1 -s -d $(ALLOW_HOST) | $(INSPECT_BIN); \
	$(SCRATCH_BIN) -d $(SCRATCH_DOMAIN) -w $(TESTENV_DIR)/wordlist.txt -hosts $(TESTENV_DIR)/hosts.txt -offline -url; \
	kill $$mock_pid 2>/dev/null || true; \
	wait $$mock_pid 2>/dev/null || true

clean:
	@rm -rf $(BIN_DIR)
