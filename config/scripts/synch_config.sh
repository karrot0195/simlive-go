#!/bin/bash

allFiles=$(find ./config -type f)

SERVER=$1

PROJECT_PATH=/root/simlive-go

if [ -z "$SERVER" ]; then
    echo "Please provide a server name as an argument."
    exit 1
fi


ssh root@$SERVER """
mkdir -p $PROJECT_PATH
"""

for file in $allFiles; do
    echo "Processing file: $file"
    scp $file root@$SERVER:$PROJECT_PATH
done

ssh root@$SERVER """
cd $PROJECT_PATH
./scripts/start_service.sh
"""


