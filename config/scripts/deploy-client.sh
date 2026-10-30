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

echo "build client"
make build-client

tar -cf ./config/dist.tar.gz ./client/dist

init_remote_state
archive_remote "$PROJECT_PATH/client/dist" client-dist .rollback_client

$SCPCMD ./config/dist.tar.gz "${SSH_USER}@${SERVER}:$PROJECT_PATH"

remote "cd '$PROJECT_PATH' && mkdir -p client && rm -rf client/dist && tar -xf dist.tar.gz && rm -f dist.tar.gz"

rm -f ./config/dist.tar.gz

remote "echo '$TS deploy-client synced client/dist' >> '$PROJECT_PATH/LOG-CHANGES.md'"
