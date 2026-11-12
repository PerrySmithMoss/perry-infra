// Run only inside the deployed app, using its actual pg/redis dependencies.
// No credentials, query rows, session contents or raw errors are printed.
import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { createRequire } from 'node:module';
import { randomUUID } from 'node:crypto';
import net from 'node:net';
import http from 'node:http';

const options = JSON.parse(process.argv[2]);
const require = createRequire(`${process.cwd()}/package.json`);
const secret = (name) => {
  const value = process.env[name];
  assert(value, 'Required connection setting is absent');
  return value.startsWith('/') ? readFileSync(value, 'utf8').trim() : value;
};

async function deniedTCP({ host, port }) {
  assert(net.isIP(host), 'A concrete IP is required');
  await new Promise((resolve, reject) => {
    const socket = net.createConnection({ host, port });
    socket.setTimeout(1500);
    socket.once('connect', () => {
      socket.destroy();
      reject(new Error('Unauthorized TCP connection succeeded'));
    });
    // Route errors, explicit refusal and timeout are all denied TCP access.
    socket.once('error', () => { socket.destroy(); resolve(); });
    socket.once('timeout', () => { socket.destroy(); resolve(); });
  });
}

// Traefik listens on all task interfaces and restricts /metrics with its
// monitoring-CIDR allowlist. On the authorized ingress peer, assert HTTP 403;
// other overlay addresses should have no route. A TCP handshake is insufficient.
async function deniedMetrics({ host, port }) {
  await new Promise((resolve, reject) => {
    const request = http.get({ host, port, path: '/metrics', timeout: 1500 }, response => {
      response.resume();
      if (response.statusCode === 403) resolve();
      else reject(new Error('Metrics were not explicitly denied'));
    });
    request.once('timeout', () => request.destroy(Object.assign(new Error('Timeout'), { code: 'ETIMEDOUT' })));
    request.once('error', error => {
      if (['ETIMEDOUT', 'ECONNREFUSED', 'EHOSTUNREACH', 'ENETUNREACH'].includes(error.code)) resolve();
      else reject(new Error('Unexpected metrics error'));
    });
  });
}

async function database() {
  const { Client } = require('pg');
  const url = new URL(secret(process.env.DATABASE_URL_FILE ? 'DATABASE_URL_FILE' : 'DATABASE_URL'));
  const expected = {
    chinwag: ['chinwag_app', 'chinwag'],
    'vita-track': ['vita_track_app', 'vita_track'],
    'im-listening': ['im_listening', 'im_listening'],
  }[options.app];
  assert.deepEqual([decodeURIComponent(url.username), url.pathname.slice(1)], expected);
  const config = {
    connectionString: url.href, connectionTimeoutMillis: 4000, query_timeout: 5000,
    ...(process.env.DATABASE_CA_FILE && { ssl: { ca: readFileSync(process.env.DATABASE_CA_FILE), rejectUnauthorized: true } }),
  };
  const client = new Client(config);
  try {
    await client.connect();
    const result = await client.query('SELECT rolsuper, rolcreatedb, rolcreaterole, rolreplication, rolbypassrls FROM pg_roles WHERE rolname = current_user');
    assert.equal(result.rows.length, 1);
    assert(Object.values(result.rows[0]).every(flag => flag === false), 'Application role is too privileged');
    await client.query('SELECT count(*) FROM public._prisma_migrations');
    await client.query('BEGIN');
    // Exercise migration DDL under the real app role, retaining no objects/data.
    const table = `__isolation_${randomUUID().replaceAll('-', '')}`;
    await client.query(`CREATE TABLE public.${table} (id integer PRIMARY KEY)`);
    await client.query(`ALTER TABLE public.${table} ADD COLUMN probe boolean`);
    await client.query('ROLLBACK');
  } finally { await client.end(); }
  if (options.final) {
    for (const name of ['chinwag', 'vita_track', 'im_listening', 'bulk_bets', 'postgres']) {
      if (name === expected[1]) continue;
      const otherURL = new URL(url.href);
      otherURL.pathname = `/${name}`;
      const other = new Client({ ...config, connectionString: otherURL.href });
      let code;
      try { await other.connect(); } catch (error) { code = error.code; }
      finally { await other.end(); }
      assert(['28000', '42501'].includes(code), 'Cross-database connection was not explicitly denied');
    }
  }
}

async function redis() {
  const { createClient } = require('redis');
  const client = createClient({ url: secret('REDIS_URL_FILE'), socket: { reconnectStrategy: false, connectTimeout: 4000 } });
  client.on('error', () => {});
  try {
    await client.connect();
    assert.equal(await client.ping(), 'PONG');
    const key = `im-listening:isolation:${randomUUID()}`;
    try {
      await client.set(key, 'probe', { EX: 30 });
      assert.equal(await client.get(key), 'probe');
    } finally { await client.del(key); }
    const other = `infra-probe:isolation:${randomUUID()}`;
    for (const call of [
      () => client.get(other),
      () => client.set(other, 'probe', { EX: 30 }),
      () => client.eval("return redis.call('GET', ARGV[1])", { keys: [], arguments: [other] }),
      () => client.sendCommand(['CONFIG', 'GET', 'maxmemory']),
      () => client.sendCommand(['SCAN', '0']),
    ]) {
      await assert.rejects(call, error => /NOPERM|ACL|no permissions|can't access/i.test(error.message));
    }
  } finally { if (client.isOpen) await client.close(); }
}

try {
  // Keep parallel probes bounded inside the small production containers.
  for (let offset = 0; offset < options.denied.length; offset += 8) {
    await Promise.all(options.denied.slice(offset, offset + 8).map(deniedTCP));
  }
  await Promise.all(options.metrics.map(deniedMetrics));
  if (options.database) await database();
  if (options.redis) await redis();
  console.log(`${options.app}: direct-IP network denials and applicable database/migration/Redis checks passed (${options.final ? 'final' : 'transition'}).`);
} catch {
  console.error('Isolation client verification failed; no private diagnostics printed.');
  process.exitCode = 1;
}
