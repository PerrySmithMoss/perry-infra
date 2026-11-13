package postgres

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Exercise the exact stack image on the VPS architecture. Every volume here is
// disposable; no production connection or data is used.
func TestOfficialRuntime(t *testing.T) {
	if os.Getenv("POSTGRES_DOCKER_TEST") != "1" {
		t.Skip("set POSTGRES_DOCKER_TEST=1 with Docker")
	}
	manifest, err := os.ReadFile("../docker-compose.stack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_, block, ok := strings.Cut(string(manifest), "\n  postgres:\n")
	if !ok {
		t.Fatal("PostgreSQL service missing")
	}
	block, _, _ = strings.Cut(block, "\n  # Administrator")
	var image string
	for line := range strings.SplitSeq(block, "\n") {
		if value, found := strings.CutPrefix(line, "    image: "); found {
			image = strings.TrimSpace(value)
		}
	}
	if !strings.HasPrefix(image, "postgres:17-bookworm@sha256:") {
		t.Fatalf("expected pinned official PostgreSQL 17 image, got %q", image)
	}
	docker := func(args ...string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		var diagnostics bytes.Buffer
		cmd.Stderr = &diagnostics
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("docker %s: %v\n%s\n%s", args[0], err, out, diagnostics.String())
		}
		return strings.TrimSpace(string(out))
	}
	ready := func(container string) {
		t.Helper()
		for range 60 {
			if exec.Command("docker", "exec", container, "pg_isready", "-h", "127.0.0.1", "-U", "postgres").Run() == nil {
				return
			}
			time.Sleep(time.Second)
		}
		t.Fatalf("PostgreSQL did not start:\n%s", docker("logs", container))
	}
	sql := func(container, query string) string {
		t.Helper()
		return docker("exec", container, "psql", "-XqAt", "-U", "postgres", "-d", "postgres", "-v", "ON_ERROR_STOP=1", "-c", query)
	}
	volume := func() string {
		v := docker("volume", "create")
		t.Cleanup(func() { exec.Command("docker", "volume", "rm", v).Run() })
		return v
	}
	start := func(v, ref string, restricted bool) string {
		t.Helper()
		args := []string{"run", "-d", "--platform", "linux/amd64", "--network", "none", "--user", "999:999", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-v", v + ":/var/lib/postgresql/data"}
		if restricted {
			args = append(args, "--read-only", "--cap-drop", "ALL", "--memory", "640m", "--cpus", "1", "--pids-limit", "256",
				"--tmpfs", "/tmp:mode=1777,size=67108864", "--tmpfs", "/var/run/postgresql:mode=1777,size=16777216",
				"--entrypoint", "sh", ref, "-ec", `
umask 077
openssl req -x509 -newkey rsa:2048 -nodes -days 1 -subj /CN=localhost \
    -addext subjectAltName=DNS:localhost -keyout /tmp/test.key -out /tmp/test.crt
exec docker-entrypoint.sh postgres -c ssl=on \
    -c ssl_cert_file=/tmp/test.crt -c ssl_key_file=/tmp/test.key`)
		} else {
			args = append(args, ref)
		}
		c := docker(args...)
		t.Cleanup(func() { exec.Command("docker", "rm", "-fv", c).Run() })
		ready(c)
		return c
	}
	const metadata = `SELECT datcollate, datctype, datlocprovider, datcollversion, pg_database_collation_actual_version(oid), pg_encoding_to_char(encoding) FROM pg_database WHERE datname = current_database();`
	const extensions = `CREATE EXTENSION citext; CREATE EXTENSION "uuid-ossp";`
	const extensionProbe = `SELECT 'Chinwag'::citext = 'chinwag'::citext AND uuid_generate_v4() IS NOT NULL;`
	check := func(c string) {
		t.Helper()
		if got := sql(c, "SHOW server_version_num"); got != "170012" {
			t.Fatalf("expected PostgreSQL 17.12, got %s", got)
		}
		if got := docker("exec", c, "id", "-u"); got != "999" {
			t.Fatalf("wrong runtime UID: %s", got)
		}
		if got := sql(c, extensionProbe); got != "t" {
			t.Fatalf("citext/UUID probe failed: %s", got)
		}
		tls := docker("exec", "-e", "PGSSLMODE=verify-full", "-e", "PGSSLROOTCERT=/tmp/test.crt", c,
			"psql", "-XqAt", "-h", "localhost", "-U", "postgres", "-v", "ON_ERROR_STOP=1", "-c",
			"SELECT ssl FROM pg_stat_ssl WHERE pid = pg_backend_pid()")
		if tls != "t" {
			t.Fatal("certificate-verified TLS probe failed")
		}
	}
	t.Log("opening deployed 17.11 data with the official image")
	{
		v := volume()
		old := start(v, "ghcr.io/perrysmithmoss/perry-infra-postgres@sha256:6d79cb70e411a80ec759a5778b76c24ab7a0b9a5f97ab125acef24ef77e76f12", false)
		sql(old, extensions+" CREATE TABLE release_probe(value text); INSERT INTO release_probe VALUES ('preserved');")
		before := sql(old, metadata)
		identifier := sql(old, "SELECT system_identifier FROM pg_control_system()")
		extensionsBefore := sql(old, "SELECT extname, extversion FROM pg_extension ORDER BY extname")
		docker("stop", old)
		upgraded := start(v, image, true)
		check(upgraded)
		if sql(upgraded, metadata) != before || sql(upgraded, "SELECT system_identifier FROM pg_control_system()") != identifier || sql(upgraded, "SELECT extname, extversion FROM pg_extension ORDER BY extname") != extensionsBefore {
			t.Fatal("database identifier, extensions or locale/collation metadata changed")
		}
		if sql(upgraded, "SELECT value FROM release_probe") != "preserved" {
			t.Fatal("existing row was not preserved")
		}
		sql(upgraded, "INSERT INTO release_probe VALUES ('official');")
		if sql(upgraded, "SELECT count(*) FROM release_probe") != "2" {
			t.Fatal("write probe failed")
		}
		if !strings.Contains(docker("exec", upgraded, "pg_dump", "-U", "postgres", "--schema-only"), "CREATE TABLE public.release_probe") {
			t.Fatal("schema dump failed")
		}
	}
	t.Log("initializing fresh data with the restricted official image")
	{
		c := start(volume(), image, true)
		sql(c, extensions)
		check(c)
		if sql(c, "SHOW server_encoding") != "UTF8" {
			t.Fatal("fresh database encoding is not UTF8")
		}
	}
}
