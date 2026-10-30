#!/bin/bash
set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
CONFIG_DIR=$(dirname "$SCRIPT_DIR")
cd "$CONFIG_DIR"

SERVER=${1:-}
if [ -z "$SERVER" ]; then
    echo "Error: Please provide a server name or IP address as an argument." >&2
    exit 1
fi

source "$SCRIPT_DIR/utils.sh" "$SERVER"

echo "synch rclone"
remote "mkdir -p /root/.config/rclone"
init_remote_state

echo "archive previous .env on server"
archive_remote_file "$PROJECT_PATH/.env" env

echo "synch rclone config"
$SCPCMD -r ./rclone "${SSH_USER}@${SERVER}:/root/.config"

echo "synch system"
$SCPCMD -r ./system/* "${SSH_USER}@${SERVER}:/etc/systemd/system/"

if [ -n "$(ls -A ./bin 2>/dev/null)" ]; then
    echo "archive previous bin on server"
    archive_remote "$PROJECT_PATH/bin" bin .rollback_bin

    echo "synch app"
    remote "rm -rf '$PROJECT_PATH/bin'; mkdir -p '$PROJECT_PATH/bin'"
    $SCPCMD -r ./bin/* "${SSH_USER}@${SERVER}:$PROJECT_PATH/bin"
else
    echo "skip bin sync: ./bin is empty (run 'make build' first)"
fi

echo "synch env"
$SCPCMD ./.env "${SSH_USER}@${SERVER}:$PROJECT_PATH"

echo "synch compose"
$SCPCMD ./docker-compose.yml "${SSH_USER}@${SERVER}:$PROJECT_PATH"

echo "synch script"
for script in enable-services.sh restart-services.sh rollback.sh; do
    $SCPCMD "./scripts/$script" "${SSH_USER}@${SERVER}:$PROJECT_PATH"
done
$SCPCMD ./journald.conf "${SSH_USER}@${SERVER}:/etc/systemd/"
remote "systemctl restart systemd-journald"

remote "echo '$TS deploy-server synced (env/system/bin/compose/scripts)' >> '$PROJECT_PATH/LOG-CHANGES.md'"
