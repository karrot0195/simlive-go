.PHONY: start-api start-planner start-engines start-partner start-all build build-client stop clean clean-logs logs restart status test fmt vet tidy help

LOG_DIR := logs
BIN_DIR := bin
GO ?= go
CLIENT_DIR := client

# Reusable: kill all processes by name
# Usage: $(call kill_if_running,name)
define kill_if_running
	@if pgrep -x "$(1)" >/dev/null 2>&1; then \
		echo "  Stopping existing $(1)..."; \
		pkill -x "$(1)" 2>/dev/null || true; \
		for i in 1 2 3 4 5 6 7 8 9 10; do \
			pgrep -x "$(1)" >/dev/null 2>&1 || break; \
			sleep 0.2; \
		done; \
		if pgrep -x "$(1)" >/dev/null 2>&1; then \
			echo "  $(1) did not exit; sending SIGKILL"; \
			pkill -9 -x "$(1)" 2>/dev/null || true; \
			sleep 0.2; \
		fi; \
		echo "  $(1) stopped."; \
	else \
		echo "  $(1) not running."; \
	fi
endef

# Reusable: start a service in the background
# Usage: $(call start_service,name,binary_path)
define start_service
	@if [ ! -x "$(2)" ]; then \
		echo "ERROR: Binary not found: $(2). Run 'make build' first." >&2; \
		exit 1; \
	fi
	@mkdir -p "$(LOG_DIR)"
	@nohup "$(2)" > "$(LOG_DIR)/$(1).log" 2>&1 &
	@sleep 0.5
	@if pgrep -x "$(1)" >/dev/null 2>&1; then \
		echo "  $(1) started - logs: $(LOG_DIR)/$(1).log"; \
	else \
		echo "  $(1) started (check logs) - logs: $(LOG_DIR)/$(1).log"; \
	fi
endef

start-api:
	@echo "Starting API..."
	@$(call kill_if_running,api)
	@$(call start_service,api,$(BIN_DIR)/api)

start-planner:
	@echo "Starting Planner..."
	@$(call kill_if_running,planner)
	@$(call start_service,planner,$(BIN_DIR)/planner)

start-engines:
	@echo "Starting Engines..."
	@$(call kill_if_running,engines)
	@$(call start_service,engines,$(BIN_DIR)/engines)

start-partner:
	@echo "Starting Partner..."
	@$(call kill_if_running,partner)
	@$(call start_service,partner,$(BIN_DIR)/partner)

start-all: build
	@echo "Starting all services..."
	@$(MAKE) start-api
	@$(MAKE) start-planner
	@$(MAKE) start-engines
	@$(MAKE) start-partner
	@echo ""
	@echo "All services started in background."
	@echo "Tail logs with: make logs  (or: tail -f $(LOG_DIR)/*.log)"


build-testredis:
	go build -o bin/testredis ./cmd/testredis/

build:
	@mkdir -p $(BIN_DIR)
	@echo "Building all services..."
	@status=0; \
	for svc in api planner engines partner; do \
		$(GO) build -o $(BIN_DIR)/$$svc ./cmd/$$svc/ & \
		pids="$$pids $$!"; \
	done; \
	for pid in $$pids; do \
		wait $$pid || status=1; \
	done; \
	exit $$status
	@echo "All services built."

build-client:
	@echo "Building client..."
	@cd $(CLIENT_DIR) && pnpm build

# Convenience targets
logs:
	@tail -f $(LOG_DIR)/*.log

stop:
	@echo "Stopping all services..."
	@for name in api planner engines partner; do \
		if pgrep -x "$$name" >/dev/null 2>&1; then \
			echo "  Stopping $$name..."; \
			pkill -x "$$name" 2>/dev/null || true; \
			for i in 1 2 3 4 5 6 7 8 9 10; do \
				pgrep -x "$$name" >/dev/null 2>&1 || break; \
				sleep 0.2; \
			done; \
			if pgrep -x "$$name" >/dev/null 2>&1; then \
				echo "  $$name did not exit; sending SIGKILL"; \
				pkill -9 -x "$$name" 2>/dev/null || true; \
			fi; \
			echo "  $$name stopped."; \
		fi; \
	done
	@rm -f "$(LOG_DIR)"/*.pid
	@echo "All services stopped."

restart:
	@$(MAKE) stop
	@$(MAKE) start-all

status:
	@for name in api planner engines partner; do \
		pids=$$(pgrep -x "$$name" 2>/dev/null | tr '\n' ' '); \
		if [ -n "$$pids" ]; then \
			echo "$$name: running (pids $$pids)"; \
		else \
			echo "$$name: not running"; \
		fi; \
	done

test:
	@$(GO) test ./...

fmt:
	@$(GO) fmt ./...

vet:
	@$(GO) vet ./...

tidy:
	@$(GO) mod tidy

clean: stop
	@rm -rf "$(BIN_DIR)" "$(LOG_DIR)"
	@echo "Cleaned."

clean-logs: stop
	@rm -rf "$(LOG_DIR)"
	@echo "Logs cleaned."

help:
	@echo "Available targets:"
	@echo "  build          Build api, planner, engines, partner"
	@echo "  build-client   Build the client"
	@echo "  start-api      Start API service"
	@echo "  start-planner  Start planner service"
	@echo "  start-engines  Start engines service"
	@echo "  start-partner  Start partner demo service"
	@echo "  start-all      Build and start all services"
	@echo "  stop           Stop all services"
	@echo "  restart        Stop then start all services"
	@echo "  status         Show service status"
	@echo "  logs           Tail service logs"
	@echo "  clean          Remove binaries and logs"
	@echo "  clean-logs     Remove logs only"
	@echo "  test           Run go tests"
	@echo "  fmt            Format Go code"
	@echo "  vet            Run go vet"
	@echo "  tidy           Run go mod tidy"
	@echo "  help           Show this help"
