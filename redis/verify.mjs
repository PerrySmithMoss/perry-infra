// Run in the deployed Im Listening image: exercises its actual Redis libraries
// and authentication Lua, using disposable keys. Never log errors/URLs/secrets.
import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire("/app/package.json");
const { createClient } = require("redis");
const { RedisStore: SessionStore } = require("connect-redis");
const { RedisStore: RateStore } = require("rate-limit-redis");
const { claimGoogleCompletion, logoutSession } = require("/app/dist/src/auth/googleCompletion.js");
const clients = [];
const cleanup = [];
let check = "starting";

async function connect(secret) {
  check = `connecting with ${secret}`;
  const client = createClient({
    url: readFileSync(`/run/secrets/${secret}`, "utf8").trim(),
    disableOfflineQueue: true,
    socket: { reconnectStrategy: false, connectTimeout: 5000 },
  });
  client.on("error", () => {});
  clients.push(client);
  await client.connect();
  return client;
}

async function denied(client, command) {
  check = `denying ${command[0]} ${command[0] === "SCRIPT" || command[0] === "CLIENT" ? command[1] : ""}`;
  await assert.rejects(client.sendCommand(command), /NOPERM|ACL|no permissions|can't access/i);
}

async function main() {
  const admin = await connect("redis_admin_url_v1");
  const im = await connect("im_listening_redis_url_v2");
  const probe = await connect("redis_probe_url_v1");
  const token = "infra-verification-" + randomUUID();
  check = "application key operations";
  const own = `im-listening:sess:${token}`;
  const other = `infra-probe:${token}`;
  cleanup.push(() => admin.del([own, other, `im-listening:rate:${token}`, `im-listening:google:completion:${token}`]));

  assert.equal(await im.ping(), "PONG");
  assert.equal(await probe.ping(), "PONG");
  await im.set(own, "session", { EX: 300 });
  await probe.set(other, "cache", { EX: 300 });
  assert.equal(await im.get(own), "session");
  assert.equal(await probe.get(other), "cache");
  for (const [client, key] of [[im, other], [probe, own]]) {
    for (const command of [["GET", key], ["SET", key, "bad"], ["DEL", key]]) {
      await denied(client, command);
    }
    for (const command of [["CONFIG", "GET", "*"], ["ACL", "LIST"], ["FLUSHDB"],
      ["FLUSHALL"], ["KEYS", "*"], ["SCAN", "0"], ["SELECT", "1"],
      ["SCRIPT", "FLUSH"], ["PUBLISH", "channel", "bad"], ["CLIENT", "LIST"]]) {
      await denied(client, command);
    }
  }
  await denied(im, ["EVAL", "return redis.call('GET', KEYS[1])", "1", other]);
  // Also verify runtime checks on keys constructed by the app's logout Lua.
  await denied(im, ["EVAL", "return redis.call('GET', ARGV[1])", "0", other]);
  const sha = await im.sendCommand(["SCRIPT", "LOAD", "return redis.call('GET', KEYS[1])"]);
  await denied(im, ["EVALSHA", sha, "1", other]);
  await denied(probe, ["EVAL", "return 1", "0"]);
  const unauthenticated = createClient({ socket: { host: "perry-infra_redis", port: 6379,
    reconnectStrategy: false, connectTimeout: 5000 } });
  unauthenticated.on("error", () => {});
  clients.push(unauthenticated);
  check = "rejecting unauthenticated connections";
  await assert.rejects(async () => {
    await unauthenticated.connect();
    await unauthenticated.ping();
  }, /NOAUTH|WRONGPASS/);
  console.log("PASS: private second-client connection and bidirectional ACL isolation (including Lua)");

  const sessions = new SessionStore({ client: im, prefix: "im-listening:sess:", ttl: 300 });
  check = "session store and Google completion";
  const value = { cookie: {}, userId: 123, sessionVersion: 1 };
  await sessions.set(token, value);
  assert.deepEqual(await sessions.get(token), value);
  await sessions.touch(token, value);
  assert.ok(await admin.ttl(own) > 0);
  const release = await claimGoogleCompletion(im, token);
  assert.equal(typeof release, "function");
  assert.equal(await claimGoogleCompletion(im, token), undefined);
  await release();
  await logoutSession({ sessionID: token, session: { destroy: done => done() } }, im);
  assert.equal(await sessions.get(token), null);

  const rates = new RateStore({ prefix: "im-listening:rate:", sendCommand: (...args) => im.sendCommand(args) });
  check = "rate limiter scripts and transactions";
  await rates.init({ windowMs: 60_000 });
  assert.equal((await rates.increment(token)).totalHits, 1);
  assert.equal((await rates.increment(token)).totalHits, 2);
  assert.equal((await rates.get(token)).totalHits, 2);
  await rates.resetKey(token);
  assert.deepEqual(await im.multi().incr(`im-listening:rate:${token}`)
    .pExpire(`im-listening:rate:${token}`, 60_000, "NX").execTyped(), [1, 1]);
  console.log("PASS: deployed session store, Google completion/logout Lua, and both rate limiting paths");

  check = "server configuration";
  const config = await admin.configGet(["appendonly", "appendfsync", "maxmemory-policy", "maxmemory", "databases"]);
  assert.equal(config.appendonly, "yes");
  assert.equal(config.appendfsync, "everysec");
  assert.equal(config["maxmemory-policy"], "noeviction");
  assert.equal(config.maxmemory, String(64 * 1024 * 1024));
  assert.equal(config.databases, "1");
  console.log("PASS: persistence, memory cap, noeviction, and DB 0 configuration");
}

try {
  await main();
} catch {
  console.error(`FAIL: shared Redis verification at ${check}`);
  process.exitCode = 1;
} finally {
  for (const action of cleanup) {
    try { await action(); } catch { process.exitCode = 1; }
  }
  for (const client of clients) {
    if (client.isOpen) client.destroy();
  }
}
