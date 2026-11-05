# Research: Partner Demo Server

## Decisions

### 1. Binary location and structure
- **Decision**: Create `cmd/partner/main.go` as a standalone HTTP server binary
- **Rationale**: Matches requested path and follows project pattern (each binary under `cmd/`)
- **Alternatives**: Add to existing binary - no, need standalone demo server

### 2. Auth implementation
- **Decision**: Simple token-based auth (login endpoint returns token, verify endpoint accepts token)
- **Rationale**: Matches clarification - simple token flow
- **Alternatives**: Full multi-step auth - more complex than needed

### 3. Endpoints
- **Decision**: Minimal fixed paths for channels/EPG and simple auth endpoints
- **Rationale**: Matches clarification - minimal fixed paths
- **Alternatives**: Configurable paths - more complex

### 4. Data source
- **Decision**: Load demo data from files on disk
- **Rationale**: Matches clarification - from files
- **Alternatives**: In-memory only - less flexible for demos

### 5. Configuration
- **Decision**: Read from `partner.yaml` (simple form) similar to how other components read it
- **Rationale**: Need to integrate with existing patterns
- **Alternatives**: Separate config file - unnecessary complexity

### 6. HTTP framework
- **Decision**: Use standard library `net/http` for simplicity (demo server)
- **Rationale**: Minimal dependencies, easy to understand
- **Alternatives**: Gin - also used in project but adds dependency; net/http is fine for simple demo
