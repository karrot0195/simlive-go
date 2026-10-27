# Quickstart: Improve Logging

## Build & Test

```bash
make build
make test
make vet
```

## Verify no V* references remain

```bash
grep -rn 'VInfo\|VWarning\|VError\|VFatalf' --include='*.go' . | grep -v 'logging.go'
```

Expected: no matches.

## Verify rclone verbose + timing

1. Set `RCLONE_VERBOSE` not needed (constructor flag). Instantiate `NewRcloneService(true)`.
2. Trigger a failing download (invalid remote path) and confirm error contains elapsed time and stderr with provider detail.
3. Trigger a successful download and confirm elapsed time is logged.