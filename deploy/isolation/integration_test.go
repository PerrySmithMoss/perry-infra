package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Disposable bridge fixtures exercise actual PostgreSQL HBA and app libraries.
// Swarm encryption/membership and live app flows still require production proof.
func TestDisposableIsolation(t *testing.T) {
	if os.Getenv("ISOLATION_DOCKER_TEST") != "1" {
		t.Skip("set ISOLATION_DOCKER_TEST=1 with Docker")
	}
	prefix := "perry-isolation-test-" + time.Now().Format("150405.000000000")
	pgImage := "postgres:17.11-bookworm"
	redisImage := "redis:8.10.3-alpine@sha256:71885d8cc6e42f2bc0aa2eb853bfc4be200b5108a46ac50e117ae1df0531fb80"
	appImage := "ghcr.io/perrysmithmoss/im-listening-api@sha256:95f4699684cda0a48ddb15038aca3505d780395ee69658109f6bac13a3e564cb"
	var containers, networks []string
	volume := prefix + "-files"
	t.Cleanup(func() {
		for i := len(containers) - 1; i >= 0; i-- {
			exec.Command("docker", "rm", "-fv", containers[i]).Run()
		}
		exec.Command("docker", "volume", "rm", volume).Run()
		for _, n := range networks {
			exec.Command("docker", "network", "rm", n).Run()
		}
	})
	run := func(input string, args ...string) (string, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", args...)
		cmd.Stdin = strings.NewReader(input)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	docker := func(input string, args ...string) string {
		t.Helper()
		out, err := run(input, args...)
		if err != nil {
			t.Fatalf("fixture %s failed: %s", args[0], out)
		}
		return out
	}
	for _, suffix := range []string{"database", "redis", "denied"} {
		n := prefix + "-" + suffix
		networks = append(networks, n)
		docker("", "network", "create", "--internal", n)
	}
	subnet := strings.TrimSpace(docker("", "network", "inspect", "--format", "{{(index .IPAM.Config 0).Subnet}}", networks[0]))
	docker("", "volume", "create", volume)
	start := func(suffix, network, image string, options, command []string) string {
		t.Helper()
		name := prefix + "-" + suffix
		containers = append(containers, name)
		args := []string{"run", "-d", "--name", name, "--network", network, "--memory", "192m", "--pids-limit", "96"}
		if image == appImage {
			args = append(args, "--platform", "linux/amd64")
		}
		args = append(args, options...)
		args = append(args, image)
		args = append(args, command...)
		docker("", args...)
		return name
	}
	seed := start("seed", networks[0], redisImage, []string{"--mount", "type=volume,source=" + volume + ",target=/fixtures", "--entrypoint", "sleep"}, []string{"600"})
	dir := t.TempDir()
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(contents), 0600); err != nil {
			t.Fatal(err)
		}
		docker("", "cp", path, seed+":/fixtures/"+name)
		docker("", "exec", seed, "chmod", "644", "/fixtures/"+name)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "fixture"}, DNSNames: []string{"postgres"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	write("ca.crt", string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})))
	write("server.key", string(pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})))
	docker("", "exec", seed, "chown", "999:999", "/fixtures/server.key")
	docker("", "exec", seed, "chmod", "400", "/fixtures/server.key")
	hba, err := os.ReadFile("../../postgres/pg_hba.conf")
	if err != nil {
		t.Fatal(err)
	}
	hbaText := string(hba)
	for _, cidr := range []string{"10.42.31.0/24", "10.42.35.0/24", "10.42.37.0/24"} {
		hbaText = strings.ReplaceAll(hbaText, cidr, subnet)
	}
	write("pg_hba.conf", hbaText)
	write("verify.mjs", clientScript)
	pg := start("postgres", networks[0], pgImage, []string{"--network-alias", "postgres", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly"}, []string{"postgres", "-c", "ssl=on", "-c", "ssl_cert_file=/fixtures/ca.crt", "-c", "ssl_key_file=/fixtures/server.key", "-c", "hba_file=/fixtures/pg_hba.conf"})
	docker("", "network", "connect", "--alias", "postgres-denied", networks[2], pg)
	for i := 0; i < 60; i++ {
		if _, err := run("", "exec", pg, "pg_isready", "-h", "postgres", "-U", "postgres"); err == nil {
			break
		}
		time.Sleep(time.Second)
	}
	// The fixture password is disposable and never used with production services.
	sql := "CREATE ROLE chinwag_app LOGIN PASSWORD 'fixture'; CREATE ROLE vita_track_app LOGIN PASSWORD 'fixture'; CREATE ROLE im_listening LOGIN PASSWORD 'fixture';\n"
	for _, pair := range [][2]string{{"chinwag", "chinwag_app"}, {"vita_track", "vita_track_app"}, {"im_listening", "im_listening"}} {
		sql += "CREATE DATABASE " + pair[0] + " OWNER " + pair[1] + ";\n\\connect " + pair[0] + "\nSET ROLE " + pair[1] + "; CREATE TABLE public._prisma_migrations(id text); RESET ROLE;\n\\connect postgres\n"
	}
	sql += "CREATE DATABASE bulk_bets;\n"
	docker(sql, "exec", "-i", pg, "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "postgres", "-d", "postgres")
	write("users.acl", "user default reset off\nuser im_listening reset on >fixture ~im-listening:* -@all +ping +get +set +del +eval +hello +client|setinfo\n")
	redis := start("redis", networks[1], redisImage, []string{"--network-alias", "perry-infra_redis", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly"}, []string{"redis-server", "--save", "", "--appendonly", "no", "--aclfile", "/fixtures/users.acl"})
	denied := start("denied", networks[2], redisImage, nil, []string{"redis-server", "--save", "", "--appendonly", "no"})
	if strings.TrimSpace(docker("", "exec", denied, "redis-cli", "PING")) != "PONG" {
		t.Fatal("negative probe target is not listening")
	}
	ip := func(name, network string) string {
		return strings.TrimSpace(docker("", "inspect", "--format", "{{(index .NetworkSettings.Networks \""+network+"\").IPAddress}}", name))
	}
	for _, app := range apps {
		if app.Database == "" {
			continue
		}
		db, role := "chinwag", "chinwag_app"
		if app.Name == "vita-track" {
			db, role = "vita_track", "vita_track_app"
		}
		if app.Name == "im-listening" {
			db, role = "im_listening", "im_listening"
		}
		write(app.Name+".url", "postgresql://"+role+":fixture@postgres:5432/"+db)
		write("redis.url", "redis://im_listening:fixture@perry-infra_redis:6379/0")
		options := []string{"--entrypoint", "sleep", "--mount", "type=volume,source=" + volume + ",target=/fixtures,readonly", "-e", "DATABASE_URL_FILE=/fixtures/" + app.Name + ".url", "-e", "REDIS_URL_FILE=/fixtures/redis.url"}
		if app.Redis != "" {
			options = append(options, "-e", "DATABASE_CA_FILE=/fixtures/ca.crt")
		}
		client := start("client-"+app.Name, networks[0], appImage, options, []string{"600"})
		config := clientOptions{App: app.Name, Database: true, Redis: app.Redis != "", Final: true, Denied: []probe{{ip(denied, networks[2]), 6379}}, Metrics: []probe{}}
		if app.Redis != "" {
			docker("", "network", "connect", networks[1], client)
		} else {
			config.Denied = append(config.Denied, probe{ip(redis, networks[1]), 6379})
		}
		encoded, _ := json.Marshal(config)
		docker("", "exec", client, "node", "/fixtures/verify.mjs", string(encoded))
		// Ensure the verifier detects reachable unauthorized peers, rather than
		// passing simply because a target IP was missing or its port was closed.
		config.Database = false
		config.Redis = false
		config.Denied = []probe{{ip(pg, networks[0]), 5432}}
		encoded, _ = json.Marshal(config)
		if _, err := run("", "exec", client, "node", "/fixtures/verify.mjs", string(encoded)); err == nil {
			t.Fatal("verifier accepted a reachable forbidden listener")
		}
	}
	for _, test := range []struct{ network, host, user, db, ssl string }{
		{networks[2], "postgres-denied", "chinwag_app", "chinwag", "disable"},
		{networks[0], "postgres", "im_listening", "im_listening", "disable"},
	} {
		dsn := "host=" + test.host + " user=" + test.user + " dbname=" + test.db + " sslmode=" + test.ssl
		out, err := run("", "run", "--rm", "--network", test.network, "-e", "PGPASSWORD=fixture", pgImage, "psql", "-XqAt", dsn, "-c", "SELECT 1")
		if err == nil || !strings.Contains(out, "pg_hba.conf rejects connection") {
			t.Fatal("HBA failed to reject wrong-source or plaintext Im Listening traffic")
		}
	}
}
