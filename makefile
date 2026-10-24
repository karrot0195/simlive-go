.PHONY: start-api start-planner start-engines start-all build build-client stop clean-logs logs

LOG_DIR := logs

# Ensure log directory exists
$(LOG_DIR):
	@mkdir -p $(LOG_DIR)

# Reusable: kill a service if its pidfile points to a live process.
# Usage: $(call kill_if_running,name)
define kill_if_running
	if [ -f "$(LOG_DIR)/$(1).pid" ]; then \
		pid=$$(cat "$(LOG_DIR)/$(1).pid"); \
		if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
			echo "Stopping existing $(1) (pid $$pid)..."; \
			kill "$$pid"; \
			while kill -0 "$$pid" 2>/dev/null; do sleep 0.2; done; \
		fi; \
		rm -f "$(LOG_DIR)/$(1).pid"; \
	fi
endef

# Reusable: start a service in the background and record its pid
# Usage: $(call start_service,name,cmdpath)
define start_service
	@nohup go run $(2) > $(LOG_DIR)/$(1).log 2>&1 & \
	echo $$! > $(LOG_DIR)/$(1).pid
	@echo "$(1) started (pid $$(cat $(LOG_DIR)/$(1).pid)) - logs: $(LOG_DIR)/$(1).log"
endef

start-api: | $(LOG_DIR)
	@echo "Starting API..."
	@$(call kill_if_running,api)
	@$(call start_service,api,./cmd/api)

start-planner: | $(LOG_DIR)
	@echo "Starting Planner..."
	@$(call kill_if_running,planner)
	@$(call start_service,planner,./cmd/planner)

start-engines: | $(LOG_DIR)
	@echo "Starting Engines..."
	@$(call kill_if_running,engines)
	@$(call start_service,engines,./cmd/engines)

start-all: start-api start-planner start-engines
	@echo ""
	@echo "All services started in background."
	@echo "Tail logs with: make logs  (or: tail -f $(LOG_DIR)/*.log)"

build:
	@mkdir -p config/bin
	@go build -o config/bin/api ./cmd/api/ & \
	wait && go build -o config/bin/planner ./cmd/planner/ & \
	wait && go build -o config/bin/engines ./cmd/engines/
	@echo "All services built."

build-client:
	cd ./client && pnpm build

# Convenience targets
logs:
	@tail -f $(LOG_DIR)/*.log

stop:
	@for f in $(LOG_DIR)/*.pid; do \
		[ -f "$$f" ] || continue; \
		pid=$$(cat $$f); \
		if kill -0 $$pid 2>/dev/null; then \
			echo "Stopping $$(basename $$f .pid) (pid $$pid)"; \
			kill $$pid; \
		fi; \
		rm -f $$f; \
	done
	@echo "All services stopped."

clean-logs:
	@rm -rf $(LOG_DIR)
	@echo "Logs cleaned."
