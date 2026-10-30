#!/bin/bash
set -e

PROJECT_PATH="${PROJECT_PATH:-/root/simlive-go}"
SERVICES="simlive-planner simlive-engines simlive-api"

systemctl daemon-reload
for svc in $SERVICES; do
    systemctl enable "$svc.service"
done

# Delegate start + health check (+ auto-rollback) to restart-services.sh when
# it has been synced by deploy-server.sh.
if [ -f "$PROJECT_PATH/restart-services.sh" ]; then
    exec bash "$PROJECT_PATH/restart-services.sh"
fi

for svc in $SERVICES; do
    systemctl start "$svc.service"
done