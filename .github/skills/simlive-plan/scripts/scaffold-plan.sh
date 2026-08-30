#!/usr/bin/env bash
# scaffold-plan.sh — create a new dated SimLive-Go plan file.
#
# Usage:
#   ./scripts/scaffold-plan.sh <feature-purpose> [full|short]
#
# Example:
#   ./scripts/scaffold-plan.sh cold-start-stall-hardening full
#
# The script copies the right template (assets/plan-template-{full,short}.md)
# into ./docs/<ddMMyy>-<feature-purpose>.md and prints the absolute path of
# the new file. It refuses to overwrite an existing file unless --force is
# passed.

set -euo pipefail

if [[ $# -lt 1 || $# -gt 3 ]]; then
  echo "usage: $0 <feature-purpose> [full|short] [--force]" >&2
  exit 2
fi

PURPOSE="$1"
SHAPE="${2:-full}"
FORCE="0"
if [[ "${3:-}" == "--force" ]]; then
  FORCE="1"
fi

# Validate purpose: kebab-case, 3-60 chars.
if ! [[ "$PURPOSE" =~ ^[a-z0-9]+(-[a-z0-9]+)*$ ]] || [[ ${#PURPOSE} -lt 3 || ${#PURPOSE} -gt 60 ]]; then
  echo "error: feature-purpose must be kebab-case (3-60 chars), got: $PURPOSE" >&2
  exit 2
fi

# Validate shape.
case "$SHAPE" in
  full|short) ;;
  *) echo "error: shape must be 'full' or 'short', got: $SHAPE" >&2; exit 2 ;;
esac

# Resolve script dir (the skill root) and repo root.
SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" &>/dev/null && pwd)"
SKILL_DIR="$(cd -- "$SCRIPT_DIR/.." &>/dev/null && pwd)"
REPO_ROOT="$(cd -- "$SKILL_DIR/../../.." &>/dev/null && pwd)"

# Date prefix (UTC, ddMMyy).
if date -u +%d%m%y >/dev/null 2>&1; then
  DATE_PREFIX="$(date -u +%d%m%y)"
else
  DATE_PREFIX="$(date -u +%d%m%y)"
fi

OUT_DIR="$REPO_ROOT/docs"
OUT_FILE="$OUT_DIR/${DATE_PREFIX}-${PURPOSE}.md"

mkdir -p "$OUT_DIR"

if [[ -e "$OUT_FILE" && "$FORCE" != "1" ]]; then
  echo "error: $OUT_FILE already exists. Re-run with --force to overwrite." >&2
  exit 1
fi

TEMPLATE="$SKILL_DIR/assets/plan-template-${SHAPE}.md"
if [[ ! -f "$TEMPLATE" ]]; then
  echo "error: template not found: $TEMPLATE" >&2
  exit 1
fi

cp "$TEMPLATE" "$OUT_FILE"
chmod +x "$SCRIPT_DIR/scaffold-plan.sh" || true
echo "$OUT_FILE"
