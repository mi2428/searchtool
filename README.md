# Search Tool

An authenticated web search API for Brave, Tavily, Exa, Firecrawl, and Parallel.

## Getting Started

The Search Tool exposes authenticated `POST /search` requests and normalizes provider results to `title`, `link`, and `snippet`.
Requests rotate through providers and fall back on timeouts, HTTP 408, HTTP 429, or HTTP 5xx; other provider errors fail closed.
HTTP 429 applies a provider cooldown using `Retry-After` (60 seconds by default, capped at one hour), and queries over 500 characters skip Firecrawl.

### Kubernetes (recommended)

[`k8s/base/`](k8s/base) contains a Deployment and ClusterIP Service.
Create a `searchtool-credentials` Secret with the provider keys and client token listed below.
The `:must-pin` image reference is an unpublished placeholder; the consuming overlay must replace it with an immutable image digest and provide an environment-specific ingress.
Keep credentials outside Kustomize, restrict ingress to intended clients, and do not use a public LoadBalancer.

The Search Tool requests 64 MiB and is limited to 256 MiB.
The Deployment uses one replica with the `Recreate` strategy because provider cooldown state is process-local.
In-progress requests can be interrupted during upgrades.

### Docker Compose

Use the `.env` file configured below.

```console
$ docker compose up --build -d searchtool
$ curl --fail http://127.0.0.1:38083/health
$ curl --fail http://127.0.0.1:38083/search \
    -H 'Authorization: Bearer <searchtool-client-token>' \
    -H 'Content-Type: application/json' \
    -d '{"query":"example search","count":5}'
```

Stop the Compose project with `docker compose down`.
The service publishes its host port on loopback only.

## Configuration

Copy `.env.example` to `.env` and replace every `replace-with-*` value.

| Variable | Default / requirement | Description |
| --- | --- | --- |
| `SEARCHTOOL_BRAVE_API_KEY` | Required | Brave Search API key |
| `SEARCHTOOL_CLIENT_TOKEN` | Required | Bearer token of at least 32 bytes |
| `SEARCHTOOL_EXA_API_KEY` | Required | Exa API key |
| `SEARCHTOOL_FIRECRAWL_API_KEY` | Required | Firecrawl API key |
| `SEARCHTOOL_LISTEN_ADDRESS` | `:8081` | HTTP listen address |
| `SEARCHTOOL_LOCAL_PORT` | `38083` | Docker Compose host port |
| `SEARCHTOOL_PARALLEL_API_KEY` | Required | Parallel API key |
| `SEARCHTOOL_TAVILY_API_KEY` | Required | Tavily API key |

Credentials are read once at startup; restart the Search Tool after changing them.

## HTTP API

| Method | Path | Authentication |
| --- | --- | --- |
| `GET` | `/health`, `/readyz` | None |
| `POST` | `/search` | Bearer token |

`POST /search` accepts `{"query":"...","count":5}`.
Queries are limited to 2048 bytes; count defaults to 5 and is limited to 20.
The response is `{"results":[{"title":"...","link":"https://...","snippet":"..."}]}`.
Invalid paths return `404`; unsupported methods return `405`.

## Development

Run `make help` to list available checks and build targets.

```console
$ make

Quality
  check        Run Go tests, static checks, container and Kubernetes validation
  fmt          Format Go source

Development
  build        Build the Search Tool
  image        Build the Search Tool image
  dev          Start the Search Tool (requires .env)
  down         Stop local Compose services

Help
  help         Show available commands
```

## License

[MIT](LICENSE).
