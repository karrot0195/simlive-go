#!/bin/bash
set -euo pipefail

make build-client

DIR=$(dirname $(dirname "$0"))
source $DIR/scripts/utils.sh

tar -cf ./config/dist.tar.gz ./client/dist

$SCPCMD ./config/dist.tar.gz root@$SERVER:$PROJECT_PATH

$SSHCMD """
 cd $PROJECT_PATH && rm -rf client/dist && tar -xf dist.tar.gz && rm dist.tar.gz
"""

rm -rf ./config/dist.tar.gz