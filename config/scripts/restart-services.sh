#!/bin/bash
set -u

PROJECT_PATH="${PROJECT_PATH:-/root/simlive-go}"
LOG="$PROJECT_PATH/LOG-CHANGES.md"
SERVICES="simlive-planner simlive-engines simlive-api"
API_URL="http://127.0.0.1:8080/"
HEALTH_TIMEOUT=15

mkdir -p "$PROJECT_PATH/archived"
[ -f "$LOG" ] || printf '# Change Log\n\n' > "$LOG"
TS=$(date +%Y%m%d-%H%M%S)

is_healthy() {
    local svc
    for svc in $SERVICES; do
        systemctl is-active --quiet "$svc" || return 1
    done
    # Any HTTP response proves the API is listening. We deliberately avoid
    # `curl -f` because `/` serves the client bundle and may 404 on servers
    # without client/dist, which is not an API failure.
    if command -v curl >/dev/null 2>&1; then
        curl -s -o /dev/null --max-time 3 "$API_URL" || return 1
    fi
    return 0
}

restart_all() {
    local svc
    systemctl daemon-reload
    for svc in $SERVICES; do
        systemctl restart "$svc.service"
    done
}

wait_healthy() {
    local i
    for i in $(seq 1 "$HEALTH_TIMEOUT"); do
        sleep 1
        is_healthy && return 0
    done
    return 1
}

report() {
    local svc
    for svc in $SERVICES; do
        if systemctl is-active --quiet "$svc"; then
            echo "  $svc: active"
        else
            echo "  $svc: INACTIVE"
        fi
    done
}

rollback_component() {
    local pointer="$1" dest="$2" kind="$3" target
    [ -f "$pointer" ] || return 0
    target=$(cat "$pointer")
    if [ ! -d "$target" ]; then
        echo "  $kind: rollback target missing ($target)"
        return 0
    fi
    echo "  rolling back $kind <- $(basename "$target")"
    if [ -e "$dest" ]; then
        mv "$dest" "$PROJECT_PATH/archived/${TS}-${kind}-failed-$$" 2>/dev/null || true
    fi
    mkdir -p "$(dirname "$dest")"
    mv "$target" "$dest"
    rm -f "$pointer"
    echo "${TS} auto-rollback $kind <- $(basename "$target")" >> "$LOG"
}

restart_all
echo "waiting for services to become healthy..."
if wait_healthy; then
    rm -f "$PROJECT_PATH/.rollback_bin" "$PROJECT_PATH/.rollback_client"
    report
    echo "${TS} restart healthy" >> "$LOG"
    exit 0
fi

echo "UNHEALTHY after restart - attempting rollback"
report

rollback_component "$PROJECT_PATH/.rollback_bin" "$PROJECT_PATH/bin" bin
rollback_component "$PROJECT_PATH/.rollback_client" "$PROJECT_PATH/client/dist" client-dist

restart_all
echo "re-checking after rollback..."
if wait_healthy; then
    report
    echo "${TS} restart after auto-rollback (healthy)" >> "$LOG"
else
    report
    echo "${TS} restart after auto-rollback (still unhealthy)" >> "$LOG"
    echo "Rollback did not restore health - manual intervention required" >&2
fi
exit 1
