// Execute with docker exec inside the production API. Exercise the deployed HTTP
// handlers using a disposable account and a reserved forwarded client address.
// Passwords and cookies stay in memory; no email or public content is created.
import assert from "node:assert/strict";
import { createHash, randomBytes, randomInt } from "node:crypto";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";

const require = createRequire("/app/package.json");
const { PrismaPg } = require("@prisma/adapter-pg");
const { PrismaClient } = require("/app/dist/src/generated/prisma/client.js");
const { createClient } = require("redis");
const read = name => readFileSync(`/run/secrets/${name}`, "utf8").trim();
const prisma = new PrismaClient({ adapter: new PrismaPg({
  connectionString: read("database_url"),
  ssl: { ca: readFileSync(process.env.DATABASE_CA_FILE) },
  connectionTimeoutMillis: 3000,
}) });
const redis = createClient({ url: read("redis_url"), socket: { reconnectStrategy: false } });
redis.on("error", () => {});
const token = randomBytes(12).toString("hex");
const email = `redis-verification-${token}@example.invalid`;
const username = `redis_check_${token}`;
const password = randomBytes(32).toString("base64url");
const ip = `198.18.${randomInt(256)}.${randomInt(1, 255)}`;
let cookie = "";
const testSessionKeys = new Set();
let phase = "initializing";

async function query(query, variables = {}, presentedCookie = cookie) {
  const response = await fetch("http://127.0.0.1:5050/graphql", {
    method: "POST",
    headers: {
      "Content-Type": "application/json", Origin: "https://im-listening.vercel.app",
      "X-Forwarded-Proto": "https", "X-Forwarded-For": ip,
      ...(presentedCookie && { Cookie: presentedCookie }),
    },
    body: JSON.stringify({ query, variables }),
    signal: AbortSignal.timeout(10000),
  });
  const values = response.headers.getSetCookie();
  if (values.length) {
    cookie = values[0].split(";", 1)[0];
    const signed = decodeURIComponent(cookie.slice(cookie.indexOf("=") + 1));
    const sid = signed.startsWith("s:") ? signed.slice(2).split(".", 1)[0] : "";
    if (/^[A-Za-z0-9_-]{32}$/.test(sid)) testSessionKeys.add(`im-listening:sess:${sid}`);
  }
  if (response.status === 429) return { status: 429, headers: response.headers };
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.errors, undefined);
  return { status: response.status, data: body.data, headers: response.headers };
}

try {
  await prisma.$connect();
  await redis.connect();
  phase = "registration and session reload";
  const created = await query(`mutation($email:String!,$password:String!,$username:String!) {
    registerUser(email:$email,password:$password,username:$username,firstName:"Redis",lastName:"Verification") {
      user { id username } errors { field message }
    }
  }`, { email, password, username });
  assert.equal(created.data.registerUser.errors, null);
  const id = created.data.registerUser.user.id;
  assert.equal(created.data.registerUser.user.username, username);
  assert.ok(cookie.startsWith("__Host-im-listening="));
  assert.equal((await query("query { getCurrentUser { id username } }")).data.getCurrentUser.id, id);
  assert.ok(created.headers.get("ratelimit"));

  phase = "logout revocation";
  const originalCookie = cookie;
  assert.equal((await query("mutation { logoutUser }")).data.logoutUser, true);
  assert.equal((await query("query { getCurrentUser { id } }", {}, originalCookie)).data.getCurrentUser, null);
  cookie = "";

  phase = "password sign-in";
  const login = `mutation($email:String!,$password:String!) { loginUser(email:$email,password:$password) { user { id } errors { field message } } }`;
  const signedIn = await query(login, { email, password });
  assert.equal(signedIn.data.loginUser.errors, null);
  assert.equal(signedIn.data.loginUser.user.id, id);
  assert.equal((await query("query { getCurrentUser { id } }")).data.getCurrentUser.id, id);
  assert.equal((await query("mutation { logoutUser }")).data.logoutUser, true);
  cookie = "";
  const rejected = await query(login, { email, password: "incorrect-password" });
  assert.equal(rejected.data.loginUser.user, null);
  assert.ok(rejected.data.loginUser.errors.length > 0);
  console.log("PASS: production HTTP registration, password sign-in/rejection, session reload, and logout revocation");

  phase = "HTTP rate limiting";
  let limited = false;
  for (let attempt = 0; attempt < 121; attempt++) {
    const result = await query("query { __typename }");
    if (result.status === 429) {
      assert.ok(Number(result.headers.get("retry-after")) > 0);
      limited = true;
      break;
    }
  }
  assert.ok(limited);
  console.log("PASS: production HTTP 429, Retry-After and rate limit headers for an isolated test address");
} catch {
  console.error(`FAIL: production HTTP verification at ${phase}`);
  process.exitCode = 1;
} finally {
  try {
    // Delete only this random, unpublished test account and its profile.
    await prisma.user.deleteMany({ where: { email, username } });
    if (redis.isReady) {
      const account = createHash("sha256").update(email).digest("hex");
      await redis.del([
        ...testSessionKeys,
        `im-listening:rate:graphql:${ip}`,
        ...["login", "register"].flatMap(action => [
          `im-listening:rate:${action}:ip:${ip}`, `im-listening:rate:${action}:account:${account}`,
        ]),
      ]);
    }
    console.log("PASS: disposable account and rate limit fixture cleaned up");
  } catch {
    console.error("FAIL: test fixture cleanup needs private inspection");
    process.exitCode = 1;
  }
  if (redis.isOpen) redis.destroy();
  await prisma.$disconnect();
}
