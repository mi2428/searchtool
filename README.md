# Search Tool

An authenticated web search API that normalizes results from Brave, Tavily, Exa, Firecrawl, and Parallel.

Search requests rotate through providers and fall back on timeouts, HTTP 408, HTTP 429, or HTTP 5xx. Other provider errors fail closed. HTTP 429 applies a provider cooldown using `Retry-After` (60 seconds by default, capped at one hour), and queries over 500 characters skip Firecrawl.

## Run

Copy `.env.example` to `.env` and replace every `replace-with-*` value.

```console
$ docker compose up --build -d
$ curl --fail http://127.0.0.1:38083/health
$ curl --fail http://127.0.0.1:38083/search \
    -H 'Authorization: Bearer <searchtool-client-token>' \
    -H 'Content-Type: application/json' \
    -d '{"query":"example search","count":5}'
```

The service publishes its port on loopback only. Stop it with `docker compose down`.

## Kubernetes

[`k8s/base/`](k8s/base) contains a Deployment and ClusterIP Service. Create a `searchtool-credentials` Secret with the required provider keys and client token. The `:must-pin` image reference is an unpublished placeholder; the consuming overlay must replace it with an immutable image digest and provide an environment-specific ingress.

The Deployment requests 64 MiB and is limited to 256 MiB. It uses one replica with the `Recreate` strategy because provider cooldown state is process-local. In-progress requests can be interrupted during upgrades.

## Configuration

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

Credentials are read once at startup; restart the service after changing them.

## HTTP API

| Method | Path | Authentication |
| --- | --- | --- |
| `GET` | `/health`, `/readyz` | None |
| `POST` | `/search` | Bearer token |

`POST /search` accepts `{"query":"...","count":5}`. Queries are limited to 2048 bytes; count defaults to 5 and is limited to 20. The response is `{"results":[{"title":"...","link":"https://...","snippet":"..."}]}`. Invalid paths return `404`; unsupported methods return `405`.

## Development

Run `make check` for tests, static checks, container validation, and Kubernetes validation. Run `make build` to build the service binary.

## License

[MIT](LICENSE).
