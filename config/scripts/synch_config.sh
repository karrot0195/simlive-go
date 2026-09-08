#!/bin/bash
set -euo pipefail

DIR=$(dirname $(dirname "$0"))

echo "access $DIR"

cd "$DIR"

SERVER=${1:-}

if [ -z "$SERVER" ]; then
    echo "Error: Please provide a server name or IP address as an argument." >&2
    exit 1
fi

PROJECT_PATH="/root/simlive-go"
SSH_KEY="~/.ssh/espx_vultr"
SSHCMD="ssh -i $SSH_KEY root@$SERVER"
SCPCMD="scp -i $SSH_KEY"

echo "synch rclone"
$SSHCMD "mkdir -p $PROJECT_PATH; mkdir -p /root/.config/rclone"
$SCPCMD -r ./rclone root@$SERVER:/root/.config

echo "synch system"
$SCPCMD -r ./system/* root@$SERVER:/etc/systemd/system/

echo "synch app"
$SCPCMD -r ./bin root@$SERVER:$PROJECT_PATH/bin

echo "synch env"
$SCPCMD -r ./.env root@$SERVER:$PROJECT_PATH

echo "synch compose"
$SCPCMD -r ./docker-compose.yml  root@$SERVER:$PROJECT_PATH

echo "synch script"
$SCPCMD -r ./scripts/start_service.sh  root@$SERVER:$PROJECT_PATH
