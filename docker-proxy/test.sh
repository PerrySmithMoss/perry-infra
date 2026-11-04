#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
image=$(awk '/image: haproxy:/{print $2}' docker-compose.stack.yaml)
name="bulkbets-proxy-test-$$"
trap 'docker rm -f "$name" >/dev/null 2>&1 || true' EXIT
# Only discovery reaches the local test daemon; no host paths except its socket.
docker run --rm -d --name "$name" --network none --read-only --cap-drop ALL --user 0:0 \
  -v /var/run/docker.sock:/var/run/docker.sock:ro \
  -v "$PWD/docker-proxy/haproxy.cfg:/usr/local/etc/haproxy/haproxy.cfg:ro" "$image" >/dev/null
assert_status() {
  expected=$1; path=$2; shift 2
  response=$(docker exec "$name" wget -S -O /dev/null -q "$@" "http://127.0.0.1:2375$path" 2>&1 || true)
  case "$response" in *"HTTP/1.1 $expected"*) ;; *) printf '%s\n' "$response" >&2; exit 1 ;; esac
}
# Wait for process readiness without issuing any mutation to the daemon.
for attempt in 1 2 3 4 5; do
  if docker exec "$name" wget -q -O /dev/null http://127.0.0.1:2375/version; then break; fi
  sleep 1
done
assert_status 200 /version
assert_status 403 /v1.41/containers/json
assert_status 403 /v1.41/secrets
assert_status 403 /v1.41/configs
assert_status 403 /v1.41/exec/test/json
assert_status 403 /v1.41/services/create --post-data='{}'
assert_status 403 /v1.41/containers/create --post-data='{}'
printf '%s\n' 'Docker API: discovery allowed; containers, credentials and writes denied.'
