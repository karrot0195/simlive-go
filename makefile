PHONY: start-api start-planner start-engines start-all

start-api:
	go run ./cmd/api/main.go

start-planner:
	go run ./cmd/planner/main.go

start-engines:
	go run ./cmd/engines/main.go

start-all:
	@make start-api & \
	make start-planner & \
	make start-engines & \
	wait

