# WhoKnows

WhoKnows is a search engine for web pages. Users can search for topics and get relevant results. Content is indexed by a web scraper that fetches pages from Wikipedia and stores them in the database. Searches with no results can be manually forwarded to the scraper by an admin via the Grafana monitoring dashboard.

## Features

- **Search** — full-text search across indexed pages, filtered by language (English/Danish)
- **User accounts** — register, log in, and log out with session-based authentication
- **Web scraping** — Wikipedia content indexed via an Azure Function triggered manually by an admin
- **Prometheus metrics** — HTTP request counts/latency and search hit/miss counters exposed at `/metrics`
- **Search log** — every search query is written to a JSON log for monitoring in Grafana
- **API** — JSON endpoints for search, user management, page ingestion, and scrape triggering
- **Swagger UI** — interactive API docs at `/swagger/`

## Architecture

```text
User browser
    │
    ▼
Nginx (HTTPS, huw.dk)
    │
    ▼
Go HTTP server (chi router, port 8080)
    │         │
    │         ▼
    │     PostgreSQL 17
    │         (pages, users)
    │
    ▼
Azure Storage Queue  ──►  Azure Function (Node.js)
                              │
                              ▼
                         Wikipedia OpenSearch API
                              │
                              ▼
                         POST /api/pages  (back to Go server)
```

An admin monitors search misses in Grafana, then manually triggers a scrape job via `POST /api/scrape`. The Go server enqueues the job to an Azure Storage Queue. The Azure Function picks it up, queries Wikipedia, and POSTs the result pages back to `/api/pages`.

## Tech stack

| Layer            | Technology                                                           |
| ---------------- | -------------------------------------------------------------------- |
| Backend          | Go 1.26, `go-chi/chi` router                                         |
| Database         | PostgreSQL 17, `pgx/v5` driver                                       |
| Migrations       | `pressly/goose` (auto-applied on startup)                            |
| Session storage  | `gorilla/sessions` cookie store                                      |
| Scraper          | Azure Functions (Node.js), Wikipedia OpenSearch API                  |
| Job queue        | Azure Storage Queue                                                  |
| Metrics          | Prometheus (`prometheus/client_golang`)                              |
| Log shipping     | Grafana Alloy → Loki                                                 |
| Monitoring       | Grafana + Prometheus + Loki on a dedicated server (`monitor.huw.dk`) |
| Reverse proxy    | Nginx with Let's Encrypt TLS                                         |
| Containerisation | Docker + Docker Compose, blue-green deployment                       |
| Infrastructure   | DigitalOcean (Terraform), Cloudflare DNS                             |
| CI/CD            | GitHub Actions (build, test, lint, blue-green deploy)                |
| IaC              | Terraform + Ansible                                                  |
| API docs         | Swagger / swaggo                                                     |

## API endpoints

| Method | Path                                        | Auth                          | Description                                    |
| ------ | ------------------------------------------- | ----------------------------- | ---------------------------------------------- |
| `GET`  | `/api/search?q=<query>&language=en` or `da` | —                             | Search indexed pages                           |
| `POST` | `/api/register`                             | —                             | Create a user account                          |
| `POST` | `/api/login`                                | —                             | Log in                                         |
| `GET`  | `/api/logout`                               | —                             | Log out                                        |
| `POST` | `/api/pages`                                | `WHOKNOWS_SCRAPER_API_KEY`    | Ingest a scraped page (used by Azure Function) |
| `POST` | `/api/scrape`                               | `WHOKNOWS_SCRAPE_TRIGGER_KEY` | Trigger a scrape job (used by admins)          |
| `GET`  | `/metrics`                                  | —                             | Prometheus metrics                             |
| `GET`  | `/swagger/*`                                | —                             | Swagger UI                                     |

### Triggering a scrape (admin)

```bash
curl -X POST https://huw.dk/api/scrape \
  -H "X-Scrape-Key: <WHOKNOWS_SCRAPE_TRIGGER_KEY>" \
  -H "Content-Type: application/json" \
  -d '{"query": "Python programming language", "language": "en"}'
```

## Local development

### Prerequisites

- Go 1.26+
- Docker + Docker Compose

### Start

```bash
cd server_go
cp .env.example .env       # fill in your local values
docker compose -f docker-compose.dev.yml up -d   # starts PostgreSQL
go run ./cmd/server
```

The server starts at `http://localhost:8080`.

### Run tests

```bash
cd server_go
go test -v ./...
```

### Add a migration

```bash
cd server_go
goose -dir migrations create <name> sql
# edit the generated file, then commit — CI validates it, deploy applies it automatically
```

## Deployment

### Ongoing deploys

Every push to `dev` triggers the GitHub Actions CD pipeline, which does a blue-green deploy to the production server.

### Spinning up a new server

Infrastructure is provisioned with Terraform (DigitalOcean) and configured with Ansible. Cloudflare DNS is updated automatically.

```bash
cd server_go
make spinup-app         # app server only (huw.dk)
make spinup-monitoring  # monitoring server only (monitor.huw.dk)
make spinup             # both servers from scratch
```

Requires `server_go/deploy/terraform/terraform.tfvars` — copy from `terraform.tfvars.example` and fill in tokens. See [`server_go/deploy/README.md`](server_go/deploy/README.md) for the full guide.

## Environment variables

| Variable                      | Used by                       | Description                                           |
| ----------------------------- | ----------------------------- | ----------------------------------------------------- |
| `DATABASE_URL`                | Go server                     | PostgreSQL connection string                          |
| `WHOKNOWS_ADDR`               | Go server                     | Bind address (default `0.0.0.0`)                      |
| `WHOKNOWS_PORT`               | Go server                     | Port (default `8080`)                                 |
| `WHOKNOWS_SCRAPER_API_KEY`    | Azure Function → `/api/pages` | Auth key for the scraper to submit pages              |
| `WHOKNOWS_SCRAPE_TRIGGER_KEY` | Admin → `/api/scrape`         | Auth key for admins to trigger scrape jobs            |
| `WHOKNOWS_SEARCH_LOG_PATH`    | Go server                     | Path for the JSON search log (default `searches.log`) |

See `.env.example` for a template.

## CI pipeline

GitHub Actions runs on every push and pull request to `dev`:

| Job                    | What it checks                                            |
| ---------------------- | --------------------------------------------------------- |
| Build & Test           | `go build` + `go test` against a live PostgreSQL instance |
| Migration Sanity Check | `goose up` against a fresh database                       |
| Go Lint                | `golangci-lint`                                           |
| Gosec                  | Static security analysis                                  |
| Govulncheck            | Known CVEs in dependencies                                |
| Super-Linter           | Dockerfile, YAML, JSON, and other non-Go files            |

## Scraper

The scraper is an **Azure Function** (Node.js) that runs when a message arrives on the Azure Storage Queue.

Flow:

1. Admin calls `POST /api/scrape` with a query and language.
2. The Go server pushes a job (`{query, language}`) onto the queue.
3. The Azure Function reads the message, calls the **Wikipedia OpenSearch API** to find matching article titles, fetches each article's extract, and POSTs each page to `POST /api/pages` on the Go server.
4. The Go server stores the page in PostgreSQL (`ON CONFLICT DO NOTHING`).

The scraper only indices Wikipedia content. Queries that have no Wikipedia results (e.g. very specific or non-encyclopaedic topics) will not produce new pages.
