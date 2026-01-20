SHELL := /bin/bash
.SHELLFLAGS := -eo pipefail -c

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
GOBIN ?= $(HOME)/go/bin

LOG_DIR ?= $(ROOT)/.logs
PIPELINE_LOG := $(LOG_DIR)/makefile.log
MOCK_PID_FILE := $(LOG_DIR)/mockenv.pid

.PHONY: help run-mockenv build

help:
	@printf "Targets:\n"
	@printf "  build              End-to-end build -> mock env -> tests -> optional install -> cleanup\n"
	@printf "  run-mockenv        Build and start mock HTTP/HTTPS/RAW services\n"

$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

run-mockenv: | $(BIN_DIR)
	$(GO) -C $(TESTENV_DIR) build -o $(ROOT)/$(MOCKENV_BIN) ./cmd/mockenv
	$(MOCKENV_BIN) -bind $(MOCK_BIND) -http $(MOCK_HTTP) -https $(MOCK_HTTPS) -raw $(MOCK_RAW) -allow $(ALLOW_HOST)

build:
	@set -euo pipefail; \
	LOG_DIR="$(LOG_DIR)"; \
	BIN_DIR="$(BIN_DIR)"; \
	ROOT="$(ROOT)"; \
	PIPELINE_LOG="$(PIPELINE_LOG)"; \
	MOCKENV_BIN="$(MOCKENV_BIN)"; \
	MOCK_BIND="$(MOCK_BIND)"; \
	MOCK_HTTP="$(MOCK_HTTP)"; \
	MOCK_HTTPS="$(MOCK_HTTPS)"; \
	MOCK_RAW="$(MOCK_RAW)"; \
	ALLOW_HOST="$(ALLOW_HOST)"; \
	SCRATCH_DOMAIN="$(SCRATCH_DOMAIN)"; \
	INSPECT_BIN="$(INSPECT_BIN)"; \
	KNOCK_BIN="$(KNOCK_BIN)"; \
	SCRATCH_BIN="$(SCRATCH_BIN)"; \
	MOCK_PID_FILE="$(MOCK_PID_FILE)"; \
	TESTENV_DIR="$(TESTENV_DIR)"; \
	GOBIN="$(GOBIN)"; \
	GO="$(GO)"; \
	BLUE="\033[1;34m"; \
	GREEN="\033[1;32m"; \
	YELLOW="\033[1;33m"; \
	RED="\033[1;31m"; \
	BOLD="\033[1m"; \
	RESET="\033[0m"; \
	step_total=10; \
	log_dir="$$LOG_DIR"; \
	mkdir -p "$$log_dir"; \
	pipeline_log="$$PIPELINE_LOG"; \
	: >"$$pipeline_log"; \
	step() { local num="$$1"; shift; printf "$${BLUE}$${BOLD}[%s/%s]➜$${RESET} %s\n" "$$num" "$$step_total" "$$*" | tee -a "$$pipeline_log"; }; \
	ok() { printf "  $${GREEN}✔$${RESET} %s\n" "$$*" | tee -a "$$pipeline_log"; }; \
	warn() { printf "  $${YELLOW}▲$${RESET} %s\n" "$$*" | tee -a "$$pipeline_log"; }; \
	fail() { printf "  $${RED}✖$${RESET} %s\n" "$$*" | tee -a "$$pipeline_log"; exit 1; }; \
	require_cmd() { command -v "$$1" >/dev/null 2>&1 || fail "Missing required command: $$1"; }; \
	require_cmd nc; \
	require_cmd grep; \
	require_cmd tee; \
	require_cmd install; \
	cleanup_mock() { \
		if [ -f "$$MOCK_PID_FILE" ]; then \
			local pid; \
			pid=$$(cat "$$MOCK_PID_FILE"); \
			if kill -0 "$$pid" 2>/dev/null; then \
				kill "$$pid" 2>/dev/null || true; \
				wait "$$pid" 2>/dev/null || true; \
			fi; \
		fi; \
		rm -f "$$MOCK_PID_FILE"; \
	}; \
	trap cleanup_mock EXIT; \
	step 1 "Building binaries into $$BIN_DIR"; \
	mkdir -p "$$BIN_DIR"; \
	: >"$$log_dir/build.log"; \
	if ! "$$GO" -C "$$ROOT/Inspect" build -o "$$ROOT/$$INSPECT_BIN" ./cmd >>"$$log_dir/build.log" 2>&1; then \
		tail -n 20 "$$log_dir/build.log" || true; \
		fail "Inspect build failed (see $$log_dir/build.log)"; \
	fi; \
	if ! "$$GO" -C "$$ROOT/Knock" build -o "$$ROOT/$$KNOCK_BIN" ./cmd >>"$$log_dir/build.log" 2>&1; then \
		tail -n 20 "$$log_dir/build.log" || true; \
		fail "Knock build failed (see $$log_dir/build.log)"; \
	fi; \
	if ! "$$GO" -C "$$ROOT/Scratch" build -o "$$ROOT/$$SCRATCH_BIN" ./cmd >>"$$log_dir/build.log" 2>&1; then \
		tail -n 20 "$$log_dir/build.log" || true; \
		fail "Scratch build failed (see $$log_dir/build.log)"; \
	fi; \
	if ! "$$GO" -C "$$ROOT/$$TESTENV_DIR" build -o "$$ROOT/$$MOCKENV_BIN" ./cmd/mockenv >>"$$log_dir/build.log" 2>&1; then \
		tail -n 20 "$$log_dir/build.log" || true; \
		fail "Mockenv build failed (see $$log_dir/build.log)"; \
	fi; \
	ok "Build complete"; \
	step 2 "Starting mock network on $$MOCK_BIND (http:$$MOCK_HTTP, https:$$MOCK_HTTPS, raw:$$MOCK_RAW)"; \
	cleanup_mock; \
	"$$MOCKENV_BIN" -bind "$$MOCK_BIND" -http "$$MOCK_HTTP" -https "$$MOCK_HTTPS" -raw "$$MOCK_RAW" -allow "$$ALLOW_HOST" >"$$log_dir/mockenv.log" 2>&1 & \
	echo $$! > "$$MOCK_PID_FILE"; \
	for _ in {1..30}; do \
		if nc -z "$$MOCK_BIND" "$$MOCK_HTTP" && nc -z "$$MOCK_BIND" "$$MOCK_HTTPS" && nc -z "$$MOCK_BIND" "$$MOCK_RAW"; then \
			ok "Mock network healthy (pid $$(cat "$$MOCK_PID_FILE"))"; \
			break; \
		fi; \
		sleep 0.2; \
	done; \
	if ! kill -0 "$$(<"$$MOCK_PID_FILE")" 2>/dev/null; then \
		fail "Mock network failed to start (see $$log_dir/mockenv.log)"; \
	fi; \
	step 3 "Inspect feature test (HTTP/HTTPS host header detection via stdin)"; \
	inspect_http_log="$$log_dir/inspect-http.log"; \
	if ! printf "127.0.0.1:$$MOCK_HTTP:Open:$$ALLOW_HOST\n127.0.0.1:$$MOCK_HTTPS:Open:$$ALLOW_HOST\n" | "$$INSPECT_BIN" | tee "$$inspect_http_log"; then \
		fail "Inspect HTTP/TLS check failed to execute (see $$inspect_http_log)"; \
	fi; \
	if ! grep -q "VULN FOUND" "$$inspect_http_log"; then \
		fail "Inspect HTTP/TLS check did not report expected finding (see $$inspect_http_log)"; \
	fi; \
	ok "Inspect HTTP/TLS checks succeeded (log: $$inspect_http_log)"; \
	step 4 "Knock feature test (desc + silent)"; \
	knock_desc_log="$$log_dir/knock-desc.log"; \
	if ! "$$KNOCK_BIN" -t 127.0.0.1 -desc >"$$knock_desc_log" 2>&1; then \
		fail "Knock description mode failed (see $$knock_desc_log)"; \
	fi; \
	if ! grep -E "8080|8443|5666" "$$knock_desc_log" >/dev/null; then \
		fail "Knock description mode missing mockenv ports (see $$knock_desc_log)"; \
	fi; \
	knock_silent_log="$$log_dir/knock-silent.log"; \
	if ! "$$KNOCK_BIN" -t 127.0.0.1 -s -d "$$ALLOW_HOST" >"$$knock_silent_log" 2>&1; then \
		fail "Knock silent mode failed (see $$knock_silent_log)"; \
	fi; \
	if ! grep -q "$$ALLOW_HOST" "$$knock_silent_log"; then \
		fail "Knock silent output missing host metadata (see $$knock_silent_log)"; \
	fi; \
	ok "Knock silent/describe modes exercised (logs in $$log_dir)"; \
	step 5 "Inspect feature test (file-driven raw banner parsing)"; \
	inspect_input="$$log_dir/inspect-input.txt"; \
	printf "127.0.0.1:$$MOCK_RAW:Open:$$ALLOW_HOST\n" >"$$inspect_input"; \
	inspect_raw_log="$$log_dir/inspect-raw.log"; \
	if ! "$$INSPECT_BIN" -f "$$inspect_input" >"$$inspect_raw_log" 2>&1; then \
		fail "Inspect raw mode failed (see $$inspect_raw_log)"; \
	fi; \
	if ! grep -q "RAW SERVICE" "$$inspect_raw_log"; then \
		fail "Inspect raw mode missing banner output (see $$inspect_raw_log)"; \
	fi; \
	ok "Inspect raw/banner path validated (log: $$inspect_raw_log)"; \
	step 6 "Scratch -> Knock -> Inspect pipeline"; \
	pipeline_chain_log="$$log_dir/scratch-knock-inspect.log"; \
	if ! "$$SCRATCH_BIN" -d "$$SCRATCH_DOMAIN" -w "$$TESTENV_DIR/wordlist-local.txt" -hosts "$$TESTENV_DIR/hosts-local.txt" -offline -ip \
		| LC_ALL=C sort -u \
		| "$$KNOCK_BIN" -s -d "$$ALLOW_HOST" \
		| "$$INSPECT_BIN" >"$$pipeline_chain_log" 2>&1; then \
		fail "Pipeline execution failed (see $$pipeline_chain_log)"; \
	fi; \
	if ! grep -q "VULN FOUND" "$$pipeline_chain_log"; then \
		fail "Pipeline did not surface Inspect findings (see $$pipeline_chain_log)"; \
	fi; \
	ok "Pipeline Scratch -> Knock -> Inspect completed (log: $$pipeline_chain_log)"; \
	step 7 "Tearing down mock network"; \
	cleanup_mock; \
	ok "Mock network stopped"; \
	step 8 "Prompting for optional install to $$GOBIN"; \
	install_choice="$${INSTALL_BINARIES:-ask}"; \
	do_install=false; \
	decision_label="no (default)"; \
	if [ "$$install_choice" = "yes" ]; then \
		do_install=true; \
		decision_label="yes (env)"; \
	elif [ "$$install_choice" = "no" ]; then \
		decision_label="no (env)"; \
	elif tty -s; then \
		read -r -p "Install binaries to $$GOBIN? [y/N] " reply; \
		case "$$reply" in \
			[yY][eE][sS]|[yY]) do_install=true; decision_label="yes (prompt)" ;; \
			*) decision_label="no (prompt)" ;; \
		esac; \
	else \
		decision_label="no (non-interactive)"; \
		warn "Non-interactive shell; skipping install. Set INSTALL_BINARIES=yes to force."; \
	fi; \
	ok "Install choice captured ($$decision_label)"; \
	step 9 "Installing binaries when approved"; \
	if [ "$$do_install" = true ]; then \
		mkdir -p "$$GOBIN"; \
		for bin in "$$INSPECT_BIN" "$$KNOCK_BIN" "$$SCRATCH_BIN" "$$MOCKENV_BIN"; do \
			install -m 0755 "$$bin" "$$GOBIN"/ || fail "Failed to install $$bin"; \
		done; \
		ok "Binaries installed to $$GOBIN"; \
	else \
		warn "Install skipped"; \
	fi; \
	step 10 "Cleaning test artifacts"; \
	cleanup_mock; \
	mkdir -p "$$log_dir"; \
	for f in inspector_findings.log knocker_history.log; do \
		if [ -f "$$f" ]; then \
			mv "$$f" "$$log_dir"/ || true; \
		fi; \
	done; \
	rm -rf "$$BIN_DIR" "$$MOCK_PID_FILE"; \
	ok "Workspace cleaned (binaries removed, logs preserved in $$log_dir)"
