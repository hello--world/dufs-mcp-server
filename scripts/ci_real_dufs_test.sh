#!/usr/bin/env bash
# Isolated real Dufs compatibility/ACL test. No official Artifact upload.
set -euo pipefail

version="v0.47.0"
archive="dufs-${version}-x86_64-unknown-linux-musl.tar.gz"
expected_archive_sha="c715c44b195f6133ca0f47e6fdc8d6015ec64b7fd9ac8e1c578ca23a92206aa4"
work="$(mktemp -d)"
dufs_pid=""
cleanup() {
    if [[ -n "${dufs_pid}" ]]; then
        kill "${dufs_pid}" 2>/dev/null || true
        wait "${dufs_pid}" 2>/dev/null || true
    fi
    rm -rf "${work}"
}
trap cleanup EXIT

curl --fail --show-error --silent --location --retry 3 \
  "https://github.com/sigoden/dufs/releases/download/${version}/${archive}" \
  --output "${work}/${archive}"
printf '%s  %s\n' "${expected_archive_sha}" "${work}/${archive}" | sha256sum --check --
tar -xzf "${work}/${archive}" -C "${work}"
chmod +x "${work}/dufs"

mkdir -p "${work}/storage/projects/apios"
# Pick a free local ephemeral port to avoid clashing with real user services.
port="$(python3 - <<'PY'
import socket
with socket.socket() as sock:
    sock.bind(("127.0.0.1", 0))
    print(sock.getsockname()[1])
PY
)"
"${work}/dufs" "${work}/storage" \
  --bind 127.0.0.1 --port "${port}" \
  --allow-upload --allow-hash --allow-archive --allow-search \
  --auth 'ci_apios:integration_only_ci@/projects/apios:rw' \
  --auth 'mcp_reader:integration_only_reader@/projects/apios:ro' \
  >"${work}/dufs.log" 2>&1 &
dufs_pid="$!"

ready=false
for _ in $(seq 1 80); do
    if ! kill -0 "${dufs_pid}" 2>/dev/null; then
        echo "Dufs exited unexpectedly" >&2
        cat "${work}/dufs.log" >&2
        exit 1
    fi
    if curl --silent --fail "http://127.0.0.1:${port}/__dufs__/health" >/dev/null; then
        ready=true
        break
    fi
    sleep 0.25
done
if [[ "${ready}" != true ]]; then
    echo "Real Dufs health timed out" >&2
    cat "${work}/dufs.log" >&2
    exit 1
fi

export DUFS_INTEGRATION_URL="http://127.0.0.1:${port}"
export DUFS_INTEGRATION_CI_USER="ci_apios"
export DUFS_INTEGRATION_CI_PASSWORD="integration_only_ci"
export DUFS_INTEGRATION_MCP_USER="mcp_reader"
export DUFS_INTEGRATION_MCP_PASSWORD="integration_only_reader"

echo "Real Dufs ${version} started: testing scoped HTTP PUT, GET, hash, anonymous rejection, and MCP read."
go test ./... -run '^TestRealDufsDirectCIAndMCP$' -count=1 -race -v
