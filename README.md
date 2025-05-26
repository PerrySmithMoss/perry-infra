# 🛠 perry-infra

Docker-based infrastructure stack for deploying and managing shared services across all my personal projects on a single VPS.

## 🚀 What's Included

This stack provides the core infrastructure needed to support multiple backend apps:

- **Traefik** – reverse proxy with automatic HTTPS (Let's Encrypt)
- **Postgres** – shared database with one DB per app
- **Redis** – shared cache using different DB indexes per app
- **Docker Swarm** – orchestration layer for app stack deployments
- **Overlay Network** – shared across all app stacks for communication

## 🐳 Deployment

### 1. Set Environment Variables

Create a `.env` file (not committed) with required values:

<details>
<summary><code>.env</code> example</summary>

```env
ACME_EMAIL=you@example.com
POSTGRES_PASSWORD=your_secure_postgres_password
```

</details>

## 2. Deploy Stack

From the VPS (or remotely over SSH if scripted), deploy the shared services using Docker Swarm:

```bash
docker stack deploy -c docker-stack.yaml infra
```

This deploys the stack under the name infra.

## 🌐 Network

All application stacks should connect to a shared overlay network to communicate with infrastructure services:

<details> <summary><code>docker-stack.yaml</code> network section example</summary>

```yaml
networks:
  shared_net:
    external: true
```

</details>

Ensure each app stack includes this to enable inter-container communication with Postgres, Redis, and Traefik.

## 🗃 Database & Cache Strategy

- Postgres: Each app uses it's own unique Postgres database (e.g., `db_1`, `db_@`).
- Redis: Each app uses a unique Redis database index (0–15) to isolate keyspaces.

> ⚠️ You must manually create new Postgres databases before deploying apps.

### Create a Postgres Database

```bash
docker exec -it $(docker ps -qf name=postgres) psql -U postgres
```

Then inside the psql shell:

```sql
CREATE DATABASE db_name;
```

## 🔐 Secrets

Sensitive credentials like database URLs, API keys, and JWT secrets are handled via:

- GitHub Actions secrets
- App-specific `.env.production` files (not committed)
- Docker Swarm secrets

## 🧩 Related Repositories

This repo supports multiple backend projects, each deployed from their own GitHub repositories:

- BulkBets
- VitaTrack
- im-listening
- chinwag
- More to come...

Each app stack connects to the shared services managed by `perry-infra`.

## 📌 Notes

- Only need to deploy this stack once, unless you update or add infrastructure services.
- App stacks are deployed independently using their own `docker-stack.yaml` files.
- All stacks reference the same shared overlay network `portfolio_network`.
