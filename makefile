.PHONY: start-api start-planner start-engines start-all build build-client stop clean clean-logs logs restart status test fmt vet tidy help

LOG_DIR := logs
BIN_DIR := config/bin
GO ?= go
CLIENT_DIR := client

# Reusable: kill a service if its pidfile points to a live process.
# Usage: $(call kill_if_running,name)
define kill_if_running
	@if [ -f "$(LOG_DIR)/$(1).pid" ]; then \
		pid=$$(cat "$(LOG_DIR)/$(1).pid"); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			echo "  Stopping existing $(1) (pid $$pid)..."; \
			kill "$$pid"; \
			for i in 1 2 3 4 5 6 7 8 9 10; do \
				kill -0 "$$pid" 2>/dev/null || break; \
				sleep 0.2; \
			done; \
			if kill -0 "$$pid" 2>/dev/null; then \
				echo "  $(1) did not exit; sending SIGKILL"; \
				kill -9 "$$pid" 2>/dev/null || true; \
			fi; \
			echo "  $(1) stopped."; \
		fi; \
		rm -f "$(LOG_DIR)/$(1).pid"; \
	fi
endef

# Reusable: start a service in the background and record its pid
# Usage: $(call start_service,name,binary_path)
define start_service
	@if [ ! -x "$(2)" ]; then \
		echo "ERROR: Binary not found: $(2). Run 'make build' first." >&2; \
		exit 1; \
	fi
	@mkdir -p "$(LOG_DIR)"
	@nohup "$(2)" > "$(LOG_DIR)/$(1).log" 2>&1 & \
	echo $$! > "$(LOG_DIR)/$(1).pid"
	@echo "  $(1) started (pid $$(cat "$(LOG_DIR)/$(1).pid")) - logs: $(LOG_DIR)/$(1).log"
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

start-all: build
	@echo "Starting all services..."
	@$(MAKE) start-api
	@$(MAKE) start-planner
	@$(MAKE) start-engines
	@echo ""
	@echo "All services started in background."
	@echo "Tail logs with: make logs  (or: tail -f $(LOG_DIR)/*.log)"

build:
	@mkdir -p $(BIN_DIR)
	@echo "Building all services..."
	@status=0; \
	for svc in api planner engines; do \
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
	@for f in "$(LOG_DIR)"/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat "$$f"); \
		name=$$(basename "$$f" .pid); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			echo "  Stopping $$name (pid $$pid)..."; \
			kill "$$pid"; \
			for i in 1 2 3 4 5 6 7 8 9 10; do \
				kill -0 "$$pid" 2>/dev/null || break; \
				sleep 0.2; \
			done; \
			if kill -0 "$$pid" 2>/dev/null; then \
				echo "  $$name did not exit; sending SIGKILL"; \
				kill -9 "$$pid" 2>/dev/null || true; \
			fi; \
			echo "  $$name stopped."; \
		fi; \
		rm -f "$$f"; \
	done
	@echo "All services stopped."

restart:
	@$(MAKE) stop
	@$(MAKE) start-all

status:
	@for f in "$(LOG_DIR)"/*.pid; do \
		[ -f "$$f" ] || continue; \
		name=$$(basename "$$f" .pid); \
		pid=$$(cat "$$f"); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			echo "$$name: running (pid $$pid)"; \
		else \
			echo "$$name: not running (stale pid $$pid)"; \
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
	@echo "  build          Build api, planner, engines"
	@echo "  build-client   Build the client"
	@echo "  start-api      Start API service"
	@echo "  start-planner  Start planner service"
	@echo "  start-engines  Start engines service"
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