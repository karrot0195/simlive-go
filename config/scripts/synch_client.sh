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


rm -rf dist dist.tar.gz
cp -r ../client/dist ./dist
tar -cf dist.tar.gz dist

$SCPCMD ./dist.tar.gz root@$SERVER:$PROJECT_PATH

$SSHCMD """
cd $PROJECT_PATH && tar -xf dist.tar.gz && rm dist.tar.gz && rm -rf ./client/dist && mv dist ./client/dist
"""