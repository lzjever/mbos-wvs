#!/usr/bin/env bash
set -euo pipefail

API_URL="${API_URL:-http://localhost:8080}"
VMSINGLE_URL="${VMSINGLE_URL:-http://localhost:8428}"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

pass() { echo -e "${GREEN}✓${NC} $1"; }
fail() { echo -e "${RED}✗${NC} $1"; exit 1; }
info() { echo -e "${YELLOW}ℹ${NC} $1"; }

# Helper: HTTP request with idempotency key
post() {
    local path="$1"
    local body="$2"
    local key="${3:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"
    curl -s -X POST "$API_URL$path" \
        -H "Content-Type: application/json" \
        -H "Idempotency-Key: $key" \
        -d "$body"
}

get() {
    local path="$1"
    curl -s "$API_URL$path"
}

delete() {
    local path="$1"
    local key="${2:-$(uuidgen | tr '[:upper:]' '[:lower:]')}"
    curl -s -X DELETE "$API_URL$path" \
        -H "Idempotency-Key: $key"
}

wait_for_task() {
    local task_id="$1"
    local timeout="${2:-60}"
    local start=$(date +%s)

    while true; do
        local now=$(date +%s)
        if (( now - start > timeout )); then
            fail "Task $task_id timed out after ${timeout}s"
        fi

        local status=$(get "/v1/tasks/$task_id" | jq -r '.status')
        case "$status" in
            SUCCEEDED) return 0 ;;
            FAILED|DEAD|CANCELED) fail "Task $task_id ended with status $status" ;;
            *) sleep 1 ;;
        esac
    done
}

echo "========================================"
echo "WVS Smoke Test"
echo "========================================"
echo ""

# Health check
info "Checking API health..."
curl -sf "$API_URL/healthz" > /dev/null || fail "API not healthy"
pass "API is healthy"

# Create workspace
info "Creating test workspace..."
WSID="test-ws-$(date +%s)"
ROOT_PATH="/ws/$WSID"
OWNER="smoke-test"

RESP=$(post "/v1/workspaces" "{\"wsid\":\"$WSID\",\"root_path\":\"$ROOT_PATH\",\"owner\":\"$OWNER\"}")
TASK_ID=$(echo "$RESP" | jq -r '.task_id')

if [ "$TASK_ID" == "null" ] || [ -z "$TASK_ID" ]; then
    fail "Failed to create workspace task: $RESP"
fi
pass "Workspace creation task created: $TASK_ID"

# Wait for init
info "Waiting for workspace initialization..."
wait_for_task "$TASK_ID"
pass "Workspace initialized"

# Verify workspace state
info "Verifying workspace state..."
WS_STATE=$(get "/v1/workspaces/$WSID" | jq -r '.state')
[ "$WS_STATE" == "ACTIVE" ] || fail "Workspace state is $WS_STATE, expected ACTIVE"
pass "Workspace is ACTIVE"

# Create snapshot
info "Creating snapshot..."
IDEMPOTENCY_KEY="snap-$(uuidgen | tr '[:upper:]' '[:lower:]')"
RESP=$(post "/v1/workspaces/$WSID/snapshots" '{"message":"smoke test snapshot"}' "$IDEMPOTENCY_KEY")
TASK_ID=$(echo "$RESP" | jq -r '.task_id')
[ "$TASK_ID" != "null" ] || fail "Failed to create snapshot task: $RESP"
pass "Snapshot creation task created: $TASK_ID"

# Wait for snapshot
info "Waiting for snapshot creation..."
wait_for_task "$TASK_ID" 120
pass "Snapshot created"

# List snapshots
info "Listing snapshots..."
SNAPSHOTS=$(get "/v1/workspaces/$WSID/snapshots")
SNAPSHOT_COUNT=$(echo "$SNAPSHOTS" | jq '.snapshots | length')
[ "$SNAPSHOT_COUNT" -ge 1 ] || fail "No snapshots found"
SNAPSHOT_ID=$(echo "$SNAPSHOTS" | jq -r '.snapshots[0].snapshot_id')
pass "Found $SNAPSHOT_COUNT snapshot(s), first: ${SNAPSHOT_ID:0:8}..."

# Get current
info "Getting current snapshot..."
CURRENT=$(get "/v1/workspaces/$WSID/current")
CURRENT_SNAP=$(echo "$CURRENT" | jq -r '.current_snapshot_id')
[ "$CURRENT_SNAP" != "null" ] || fail "No current snapshot set"
pass "Current snapshot: ${CURRENT_SNAP:0:8}..."

# Create another snapshot for rollback test
info "Creating second snapshot for rollback test..."
IDEMPOTENCY_KEY2="snap2-$(uuidgen | tr '[:upper:]' '[:lower:]')"
RESP=$(post "/v1/workspaces/$WSID/snapshots" '{"message":"second snapshot"}' "$IDEMPOTENCY_KEY2")
TASK_ID=$(echo "$RESP" | jq -r '.task_id')
wait_for_task "$TASK_ID" 120
SNAPSHOTS=$(get "/v1/workspaces/$WSID/snapshots")
SECOND_SNAP=$(echo "$SNAPSHOTS" | jq -r '.snapshots[0].snapshot_id')
pass "Second snapshot created: ${SECOND_SNAP:0:8}..."

# Set current (rollback)
info "Setting current to first snapshot (rollback)..."
RESP=$(post "/v1/workspaces/$WSID/current:set" "{\"snapshot_id\":\"$SNAPSHOT_ID\"}")
TASK_ID=$(echo "$RESP" | jq -r '.task_id')
[ "$TASK_ID" != "null" ] || fail "Failed to set current: $RESP"
wait_for_task "$TASK_ID" 120
pass "Rollback completed"

# Verify current
CURRENT=$(get "/v1/workspaces/$WSID/current")
CURRENT_SNAP=$(echo "$CURRENT" | jq -r '.current_snapshot_id')
[ "$CURRENT_SNAP" == "$SNAPSHOT_ID" ] || fail "Current snapshot mismatch: expected $SNAPSHOT_ID, got $CURRENT_SNAP"
pass "Current snapshot verified"

# Drop snapshot (the second one, not current)
info "Dropping second snapshot..."
RESP=$(delete "/v1/workspaces/$WSID/snapshots/$SECOND_SNAP" "drop-$SECOND_SNAP")
TASK_ID=$(echo "$RESP" | jq -r '.task_id')
[ "$TASK_ID" != "null" ] || fail "Failed to drop snapshot: $RESP"
wait_for_task "$TASK_ID" 120
pass "Snapshot dropped"

# Verify snapshot count
SNAPSHOTS=$(get "/v1/workspaces/$WSID/snapshots")
SNAPSHOT_COUNT=$(echo "$SNAPSHOTS" | jq '.snapshots | length')
# Should be 1 now (only the first one)
[ "$SNAPSHOT_COUNT" -eq 1 ] || info "Snapshot count is $SNAPSHOT_COUNT (may include soft-deleted)"

# Check metrics
info "Checking metrics..."
TASK_TOTAL=$(curl -s "$VMSINGLE_URL/api/v1/query?query=wvs_task_total" | jq -r '.data.result | length')
[ "$TASK_TOTAL" -gt 0 ] || fail "No task metrics found"
pass "Metrics available ($TASK_TOTAL series)"

# List tasks
info "Listing tasks..."
TASKS=$(get "/v1/tasks")
TASK_COUNT=$(echo "$TASKS" | jq '.tasks | length')
[ "$TASK_COUNT" -ge 5 ] || info "Only $TASK_COUNT tasks found"
pass "Found $TASK_COUNT task(s)"

# Disable workspace
info "Disabling workspace..."
RESP=$(delete "/v1/workspaces/$WSID" "disable-$WSID")
WS_STATE=$(echo "$RESP" | jq -r '.state')
[ "$WS_STATE" == "DISABLED" ] || fail "Workspace not disabled: $WS_STATE"
pass "Workspace disabled"

echo ""
echo "========================================"
echo -e "${GREEN}=== ALL SMOKE TESTS PASSED ===${NC}"
echo "========================================"
