# Search Tool

[![CI](https://github.com/mi2428/searchtool/actions/workflows/ci.yml/badge.svg)](https://github.com/mi2428/searchtool/actions/workflows/ci.yml)

A web search API for Brave, Tavily, Exa, Firecrawl, and Parallel, with provider rotation and automatic failover.

> [!CAUTION]
> **No built-in authentication.** Restrict access to a private network (for example, Tailscale ACLs); never expose it publicly.

## Getting Started

`POST /search` accepts `{"query":"...","count":5}`.
Queries are limited to 2048 bytes; count defaults to 5 and is limited to 20.
Invalid paths return `404`; unsupported methods return `405`.

Requests fail over on timeouts, HTTP 408, HTTP 429, or HTTP 5xx; other provider errors fail closed.
HTTP 429 applies a `Retry-After` cooldown (60 seconds by default, capped at one hour). Queries over 500 characters skip Firecrawl.

### Kubernetes (recommended)

Use [`k8s/base/`](k8s/base) with:

- A separately managed `searchtool-credentials` Secret containing the provider API keys.
- An overlay replacing the unpublished `:must-pin` image placeholder with an immutable image digest.
- Private ingress restricted to intended clients, not a public LoadBalancer.

Upgrades can interrupt in-progress requests.

### Docker Compose

```sh
cp .env.example .env
```

Set at least one provider API key in `.env` and leave unused keys empty.
See [`.env.example`](.env.example) for local settings.

```sh
docker compose up --build -d searchtool
curl --fail http://127.0.0.1:38083/search -H 'Content-Type: application/json' -d '{"query":"example search","count":5}'
```

## Environment Variables

Only configured providers participate in round-robin searches; startup fails if none are configured.

| Variable | Default / requirement | Purpose |
| --- | --- | --- |
| `SEARCHTOOL_BRAVE_API_KEY` | Optional | Brave Search API key |
| `SEARCHTOOL_TAVILY_API_KEY` | Optional | Tavily API key |
| `SEARCHTOOL_EXA_API_KEY` | Optional | Exa API key |
| `SEARCHTOOL_FIRECRAWL_API_KEY` | Optional | Firecrawl API key |
| `SEARCHTOOL_PARALLEL_API_KEY` | Optional | Parallel API key |
| `SEARCHTOOL_LISTEN_ADDRESS` | `:8081` | HTTP listen address |
| `SEARCHTOOL_LOCAL_PORT` | `38083` | Compose host port (loopback only) |

## Metrics

`GET /metrics` exposes Prometheus text metrics without authentication; restrict access at the network boundary.
Search queries, URLs, and credentials are never included in labels. Health checks and scrapes are not counted as searches.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `searchtool_requests_total` | `status_code` | Completed `/search` requests, including invalid requests and capacity rejections |
| `searchtool_provider_attempts_total` | `provider`, `status_code` | Provider attempts including failover; status `0` means no HTTP response |
| `searchtool_provider_duration_seconds` | `provider` | Attempt duration histogram, including body reads and decoding |
| `searchtool_in_flight_requests` | — | Searches holding a concurrency permit; `/metrics` bypasses this limit |
| `searchtool_provider_cooldown_seconds` | `provider` | Remaining rate-limit cooldown |

## License

[MIT](LICENSE).
