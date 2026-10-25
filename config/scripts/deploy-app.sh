#!/bin/bash
set -euo pipefail

echo "build all app"
make build

DIR=$(dirname $(dirname "$0"))
source $DIR/scripts/utils.sh

echo "synch app"
$SSHCMD "rm -rf $PROJECT_PATH/bin/*"
$SCPCMD -r $DIR/bin/* root@$SERVER:$PROJECT_PATH/bin
