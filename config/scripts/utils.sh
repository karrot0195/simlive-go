#!/bin/bash

SERVER=${1:-}

if [ -z "$SERVER" ]; then
    echo "Error: Please provide a server name or IP address as an argument." >&2
    exit 1
fi

PROJECT_PATH="${PROJECT_PATH:-/root/simlive-go}"
SSH_USER="${SSH_USER:-root}"
# NOTE: "$HOME" (not "~") so the path survives variable expansion.
SSH_KEY="${SSH_KEY:-$HOME/.ssh/id_rsa}"
SSHCMD="ssh -i $SSH_KEY $SSH_USER@$SERVER"
SCPCMD="scp -i $SSH_KEY"
TS="${TS:-$(date +%Y%m%d-%H%M%S)}"

remote() { $SSHCMD "$@"; }

init_remote_state() {
    remote "mkdir -p '$PROJECT_PATH/archived'
            [ -f '$PROJECT_PATH/LOG-CHANGES.md' ] || printf '# Change Log\n\n' > '$PROJECT_PATH/LOG-CHANGES.md'"
}

# archive_remote <server-path> <component> [pointer-file]
# Moves <server-path> -> archived/<TS>-<component>. Sets the rollback pointer
# only if it is not already set (so an earlier archive from the same deploy
# session is preserved).
archive_remote() {
    local path="$1" comp="$2" pointer="${3:--}"
    remote "if [ -e '$path' ] && { [ -f '$path' ] || [ -n \"\$(ls -A '$path' 2>/dev/null)\" ]; }; then
                mv '$path' '$PROJECT_PATH/archived/$TS-$comp'
                echo '$TS archived $comp -> archived/$TS-$comp' >> '$PROJECT_PATH/LOG-CHANGES.md'
                if [ '$pointer' != '-' ] && [ ! -f '$PROJECT_PATH/$pointer' ]; then
                    echo '$PROJECT_PATH/archived/$TS-$comp' > '$PROJECT_PATH/$pointer'
                fi
            fi"
}

# archive_remote_file <server-path> <component>
# Copies <server-path> -> archived/<TS>-<component>, keeping the original.
archive_remote_file() {
    local path="$1" comp="$2"
    remote "if [ -f '$path' ]; then
                cp '$path' '$PROJECT_PATH/archived/$TS-$comp'
                echo '$TS archived $comp -> archived/$TS-$comp' >> '$PROJECT_PATH/LOG-CHANGES.md'
            fi"
}
