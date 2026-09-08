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

echo "synch app"
$SCPCMD -r ./bin root@$SERVER:$PROJECT_PATH/bin
