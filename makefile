PHONY: start-api start-planner start-engines start-all

start-api:
	go run ./cmd/api/main.go

start-planner:
	go run ./cmd/planner/main.go

start-engines:
	go run ./cmd/engines/main.go

build:
	@go build -o config/bin/api ./cmd/api/main.go & \
	wait && go build -o config/bin/planner ./cmd/planner/main.go & \
	wait && go build -o config/bin/engines ./cmd/engines/main.go 
	@echo "All services built."

start-all:
	@make start-api & \
	make start-planner & \
	make start-engines & \
	wait

