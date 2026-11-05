#!/bin/sh
# Verify the candidate on an isolated volume initialized by the stock image.
set -eu
[ "$#" -eq 2 ] || { echo 'Usage: test-no-gosu.sh BASE_IMAGE CANDIDATE_IMAGE' >&2; exit 1; }
base=$1 candidate=$2
volume=$(docker volume create)
old= new=
cleanup() {
    if [ -n "$new" ]; then docker rm -f "$new" >/dev/null; fi
    if [ -n "$old" ]; then docker rm -f "$old" >/dev/null; fi
    docker volume rm "$volume" >/dev/null
}
trap cleanup EXIT
ready() {
    for attempt in $(seq 1 40); do
        if docker exec -u 999:999 "$1" pg_isready -h 127.0.0.1 >/dev/null; then return 0; fi
        sleep 1
    done
    docker logs "$1" >&2
    return 1
}
sql() {
    docker exec -u 999:999 "$1" psql -X -U postgres -d postgres -v ON_ERROR_STOP=1 -Atc "$2"
}
old=$(docker run -d --network none --user 999:999 -e POSTGRES_HOST_AUTH_METHOD=trust -v "$volume:/var/lib/postgresql/data" "$base")
ready "$old"
sql "$old" "CREATE TABLE release_probe (value text); INSERT INTO release_probe VALUES ('stock volume');"
metadata="SELECT current_setting('server_version'), datcollate, datctype, datlocprovider, datcollversion, pg_database_collation_actual_version(oid), pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database();"
before=$(sql "$old" "$metadata")
docker stop "$old" >/dev/null
new=$(docker run -d --network none --user 999:999 --read-only --cap-drop ALL --tmpfs /tmp:mode=1777 --tmpfs /var/run/postgresql:mode=1777 -v "$volume:/var/lib/postgresql/data" "$candidate")
ready "$new"
docker exec "$new" sh -ec 'test "$(id -u)" = 999; ! command -v gosu'
test "$(sql "$new" "SELECT value FROM release_probe;")" = "stock volume"
test "$(sql "$new" "$metadata")" = "$before"
sql "$new" "INSERT INTO release_probe VALUES ('candidate volume');"
test "$(sql "$new" "SELECT count(*) FROM release_probe;")" = 2
