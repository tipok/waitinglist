#!/usr/bin/env bash
#
# test_waitinglist.sh — Populates the waiting list with test users and polls
# until the scheduler grants them access.
#
# Usage:
#   ./scripts/test_waitinglist.sh [BASE_URL] [DATABASE_URL]
#
# Defaults:
#   BASE_URL     = http://localhost:8080
#   DATABASE_URL = postgres://brain:brain@localhost:5432/waitinglist?sslmode=disable
#
# Prerequisites:
#   - The waitinglist server must be running.
#   - curl and psql must be available on the PATH.

set -euo pipefail

BASE_URL="${1:-http://localhost:8080}"
DATABASE_URL="${2:-postgres://brain:brain@localhost:5432/waitinglist?sslmode=disable}"

POLL_INTERVAL=5   # seconds between polls
POLL_TIMEOUT=600  # give up after 10 minutes

# ── colours ──────────────────────────────────────────────────────────────────
GREEN='\033[0;32m'
YELLOW='\033[0;33m'
CYAN='\033[0;36m'
RED='\033[0;31m'
NC='\033[0m' # no colour

info()  { printf "${CYAN}[INFO]${NC}  %s\n" "$*"; }
ok()    { printf "${GREEN}[OK]${NC}    %s\n" "$*"; }
warn()  { printf "${YELLOW}[WARN]${NC}  %s\n" "$*"; }
fail()  { printf "${RED}[FAIL]${NC}  %s\n" "$*"; }

# ── helper: add a user to the waiting list ───────────────────────────────────
add_user() {
  local firstname="$1" lastname="$2" email="$3"
  info "Adding ${firstname} ${lastname} <${email}> ..."

  local response http_code body
  response=$(curl -s -w "\n%{http_code}" -X POST "${BASE_URL}/waitinglist" \
    -H "Content-Type: application/json" \
    -d "{\"firstname\":\"${firstname}\",\"lastname\":\"${lastname}\",\"email\":\"${email}\"}")

  http_code=$(echo "$response" | tail -n1)
  body=$(echo "$response" | sed '$d')

  if [ "$http_code" -eq 201 ]; then
    ok "Created (HTTP ${http_code}): ${body}"
  elif [ "$http_code" -eq 409 ]; then
    warn "Already on waiting list (HTTP ${http_code}): ${body}"
  else
    fail "Unexpected response (HTTP ${http_code}): ${body}"
  fi
}

# ── helper: fetch waiting list via API ───────────────────────────────────────
get_waitinglist() {
  curl -s -X GET "${BASE_URL}/waitinglist"
}

# ── helper: query user access status directly from the database ──────────────
query_user_access() {
  psql "${DATABASE_URL}" -t -A -c \
    "SELECT email, has_access FROM user_entity ORDER BY email;"
}

# ── 1. Add test users ───────────────────────────────────────────────────────
echo ""
echo "════════════════════════════════════════════════════════════════"
echo "  Waiting List Test Script"
echo "  Server : ${BASE_URL}"
echo "  DB     : ${DATABASE_URL}"
echo "════════════════════════════════════════════════════════════════"
echo ""

info "Adding test users to the waiting list ..."
echo ""

add_user "Alice"   "Anderson" "alice@test.local"
add_user "Bob"     "Brown"    "bob@test.local"
add_user "Charlie" "Clark"    "charlie@test.local"
add_user "Diana"   "Davis"    "diana@test.local"
add_user "Eve"     "Evans"    "eve@test.local"
add_user "Alice1"   "Anderson" "alice1@test.local"
add_user "Bob1"     "Brown"    "bob1@test.local"
add_user "Charlie1" "Clark"    "charlie1@test.local"
add_user "Diana1"   "Davis"    "diana1@test.local"
add_user "Eve1"     "Evans"    "eve1@test.local"

echo ""

# ── 2. Show current waiting list ────────────────────────────────────────────
info "Current waiting list:"
get_waitinglist | python3 -m json.tool 2>/dev/null || get_waitinglist
echo ""

# ── 3. Show user access status from the database ────────────────────────────
info "User access status (from database):"
query_user_access
echo ""

# ── 4. Poll until the waiting list is empty (scheduler grants access) ───────
info "Polling every ${POLL_INTERVAL}s until the waiting list is empty (timeout: ${POLL_TIMEOUT}s) ..."
echo "    Press Ctrl+C to stop."
echo ""

elapsed=0
while true; do
  wl=$(get_waitinglist)
  count=$(echo "$wl" | python3 -c "import sys,json; print(len(json.load(sys.stdin)))" 2>/dev/null || echo "?")

  if [ "$count" = "0" ]; then
    ok "Waiting list is empty — all users have been granted access!"
    echo ""
    info "Final user access status (from database):"
    query_user_access
    echo ""
    ok "Done."
    exit 0
  fi

  info "[${elapsed}s] Waiting list still has ${count} entries. Next check in ${POLL_INTERVAL}s ..."
  sleep "$POLL_INTERVAL"
  elapsed=$((elapsed + POLL_INTERVAL))

  if [ "$elapsed" -ge "$POLL_TIMEOUT" ]; then
    fail "Timeout reached (${POLL_TIMEOUT}s). Waiting list still has entries."
    echo ""
    info "Remaining waiting list:"
    echo "$wl" | python3 -m json.tool 2>/dev/null || echo "$wl"
    echo ""
    info "User access status (from database):"
    query_user_access
    exit 1
  fi
done
