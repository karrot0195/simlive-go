#!/bin/bash
set -euo pipefail

REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
cd "$REPO_ROOT"

SERVER=${1:-}
if [ -z "$SERVER" ]; then
    echo "Error: Please provide a server name or IP address as an argument." >&2
    exit 1
fi

source "$REPO_ROOT/config/scripts/utils.sh" "$SERVER"

echo "build all app"
make build

echo "synch app"
init_remote_state
archive_remote "$PROJECT_PATH/bin" bin .rollback_bin

remote "rm -rf '$PROJECT_PATH/bin'; mkdir -p '$PROJECT_PATH/bin'"
$SCPCMD -r "$REPO_ROOT"/config/bin/* "${SSH_USER}@${SERVER}:$PROJECT_PATH/bin"

remote "echo '$TS deploy-app synced bin' >> '$PROJECT_PATH/LOG-CHANGES.md'"
echo "rollback pointer: $PROJECT_PATH/.rollback_bin (set only if a previous bin existed)"
