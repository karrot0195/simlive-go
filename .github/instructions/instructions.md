# Implementation Rules

Always after any code change:

1. `go build ./...` — must pass
2. `golangci-lint run ./...` — must pass (fix all errors)
3. Only then commit / complete

Never skip build or lint checks.
