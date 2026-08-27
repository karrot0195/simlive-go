PHONY: start-api start-planner start-engines

start-api:
	go run ./cmd/api/main.go

start-planner:
	go run ./cmd/planner/main.go

start-engines: 
	go run ./cmd/engines/main.go
