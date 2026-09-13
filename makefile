PHONY: start-api start-planner start-engines start-all

start-api:
	go run ./cmd/api

start-planner:
	go run ./cmd/planner

start-engines:
	go run ./cmd/engines

build:
	@go build -o config/bin/api ./cmd/api/ & \
	wait && go build -o config/bin/planner ./cmd/planner/ & \
	wait && go build -o config/bin/engines ./cmd/engines/ 
	@echo "All services built."

build-client:
	cd ./client && pnpm build

start-all:
	@make start-api & \
	make start-planner & \
	make start-engines & \
	wait

