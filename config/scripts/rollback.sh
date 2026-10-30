#!/bin/bash
set -euo pipefail

PROJECT_PATH="${PROJECT_PATH:-/root/simlive-go}"
LOG="$PROJECT_PATH/LOG-CHANGES.md"
SERVICES="simlive-planner simlive-engines simlive-api"

# usage: rollback.sh [archive-name | archive-path | latest]
if [ -z "${1:-}" ]; then
    echo "Available archives (newest first):"
    ls -1t "$PROJECT_PATH/archived" 2>/dev/null || echo "(none)"
    echo
    echo "Usage: $0 <archive-name|archive-path|latest>"
    exit 0
fi

ARG="$1"
if [ "$ARG" = "latest" ]; then
    ARG=$(ls -1t "$PROJECT_PATH/archived" 2>/dev/null | head -n1 || true)
    if [ -z "$ARG" ]; then
        echo "No archives found" >&2
        exit 1
    fi
fi

case "$ARG" in
    /*) TARGET="$ARG" ;;
    *) TARGET="$PROJECT_PATH/archived/$ARG" ;;
esac

if [ ! -e "$TARGET" ]; then
    echo "Archive not found: $TARGET" >&2
    exit 1
fi

TS=$(date +%Y%m%d-%H%M%S)
NAME=$(basename "$TARGET")
echo "Rolling back from $TARGET"

stop_all() {
    local svc
    for svc in $SERVICES; do
        systemctl stop "$svc.service" 2>/dev/null || true
    done
}

start_all() {
    local svc
    systemctl daemon-reload
    for svc in $SERVICES; do
        systemctl restart "$svc.service" 2>/dev/null || true
    done
}

[ -f "$LOG" ] || printf '# Change Log\n\n' > "$LOG"

# Archive names are <TS>-<component> or <TS>-<component>-<tag> (auto-rollback
# writes -failed-<pid>, manual rolls write -pre-rollback).
case "$NAME" in
    *-bin|*-bin-*)
        stop_all
        if [ -d "$PROJECT_PATH/bin" ]; then
            mv "$PROJECT_PATH/bin" "$PROJECT_PATH/archived/${TS}-bin-pre-rollback"
        fi
        mv "$TARGET" "$PROJECT_PATH/bin"
        rm -f "$PROJECT_PATH/.rollback_bin"
        echo "${TS} rollback bin <- $NAME" >> "$LOG"
        ;;
    *-client-dist|*-client-dist-*)
        systemctl stop simlive-api.service 2>/dev/null || true
        if [ -d "$PROJECT_PATH/client/dist" ]; then
            mv "$PROJECT_PATH/client/dist" "$PROJECT_PATH/archived/${TS}-client-dist-pre-rollback"
        fi
        mkdir -p "$PROJECT_PATH/client"
        mv "$TARGET" "$PROJECT_PATH/client/dist"
        rm -f "$PROJECT_PATH/.rollback_client"
        echo "${TS} rollback client/dist <- $NAME" >> "$LOG"
        ;;
    *-env|*-env-*)
        if [ -f "$PROJECT_PATH/.env" ]; then
            cp "$PROJECT_PATH/.env" "$PROJECT_PATH/archived/${TS}-env-pre-rollback"
        fi
        cp "$TARGET" "$PROJECT_PATH/.env"
        echo "${TS} rollback .env <- $NAME" >> "$LOG"
        ;;
    *)
        echo "Unknown archive type: $NAME" >&2
        exit 1
        ;;
esac

start_all
echo "Rollback done. Verify: systemctl status simlive-* && curl -s -o /dev/null http://127.0.0.1:8080/"
