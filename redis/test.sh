#!/bin/sh
# Disposable local fixtures only. No production Docker context or published port.
set -eu
cd "$(dirname "$0")/.."
redis_image=redis:8.10.3-alpine@sha256:71885d8cc6e42f2bc0aa2eb853bfc4be200b5108a46ac50e117ae1df0531fb80
# The current production app image supplies the actual session/rate/Lua clients.
api_image=${REDIS_TEST_API_IMAGE:-ghcr.io/perrysmithmoss/im-listening-api@sha256:ebe03382ad05a5092166e33aa5b3f6c1d2a4f45b9780345ce3914d0ffd65d44e}
fixture=$(mktemp -d)
suffix=$(basename "$fixture" | tr '[:upper:].' '[:lower:]-')
network=redis-test-$suffix
volume=redis-test-$suffix
server=redis-test-$suffix
cleanup() {
  docker rm -f "$server" >/dev/null 2>&1 || :
  docker volume rm "$volume" >/dev/null 2>&1 || :
  docker network rm "$network" >/dev/null 2>&1 || :
  rm -rf "$fixture"
}
trap cleanup EXIT
trap 'exit 1' HUP INT TERM
go -C redis run . credentials "$fixture/secrets"
# Random disposable secrets remain beneath the private mktemp directory. Allow
# the non-root fixture containers to read their bind mount and nothing else.
chmod 755 "$fixture/secrets"
chmod 644 "$fixture/secrets/"*
docker network create --internal "$network" >/dev/null
docker volume create "$volume" >/dev/null
docker run --rm --network none --user 0:0 -v "$volume:/data" "$redis_image" chown 999:1000 /data
docker run -d --name "$server" --network "$network" --network-alias perry-infra_redis \
  --user 999:1000 --read-only --cap-drop ALL --memory 192m --pids-limit 64 \
  -v "$volume:/data" -v "$fixture/secrets:/run/secrets:ro" \
  -v "$PWD/redis/redis.conf:/etc/redis/redis.conf:ro" \
  "$redis_image" redis-server /etc/redis/redis.conf \
  --aclfile /run/secrets/redis_users_v1 >/dev/null
for _attempt in $(seq 1 30); do
  if docker exec "$server" sh -c 'REDISCLI_AUTH=$(cat /run/secrets/redis_healthcheck_password_v1) redis-cli --user healthcheck ping' 2>/dev/null | grep -qx PONG; then
    break
  fi
  sleep 1
done
docker run --rm --network "$network" --read-only --cap-drop ALL --user 0:0 \
  --platform linux/amd64 \
  -v "$fixture/secrets:/run/secrets:ro" -v "$PWD/redis/verify.mjs:/app/shared-redis-verify.mjs:ro" \
  --entrypoint node "$api_image" /app/shared-redis-verify.mjs

# The health user cannot read session contents.
docker exec "$server" sh -c 'REDISCLI_AUTH=$(cat /run/secrets/redis_healthcheck_password_v1) redis-cli --user healthcheck GET im-listening:sess:sentinel' | grep -q NOPERM
REDIS_TEST_CONTAINER=$server REDIS_TEST_SECRETS=$fixture/secrets \
  go -C redis test -v -run '^TestAOFReplayWithPreservationProof$' .
