#!/usr/bin/env bash
set -euo pipefail

MOUNT_PATH="${EXECUTOR_MOUNT_PATH:-/ws}"

# Format JuiceFS volume (idempotent - skips if already formatted)
juicefs format \
  --storage minio \
  --bucket "${MINIO_ENDPOINT}/${MINIO_BUCKET}" \
  --access-key "${MINIO_ACCESS_KEY}" \
  --secret-key "${MINIO_SECRET_KEY}" \
  "${JFS_META_URL}" \
  wvs-data 2>/dev/null || true

# Mount JuiceFS
mkdir -p "${MOUNT_PATH}"
juicefs mount \
  "${JFS_META_URL}" \
  "${MOUNT_PATH}" \
  --metrics "0.0.0.0:9567" \
  --background

# Wait for mount to be ready
for i in $(seq 1 30); do
  if mountpoint -q "${MOUNT_PATH}"; then
    echo "JuiceFS mounted at ${MOUNT_PATH}"
    break
  fi
  sleep 1
done

# Start executor
exec wvs-executor
