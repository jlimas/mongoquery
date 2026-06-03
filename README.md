# mongoquery

A read-only MongoDB query CLI for inspecting configured environments and services from the terminal. Designed for safe, ad-hoc investigation: connections should use accounts with the `read` role only, and the tool rejects aggregation stages and operators that mutate data or execute server-side code.

## Features

- `find`, `aggregate`, and `collections` subcommands
- Per-environment / per-service connection configuration via YAML
- Refuses forbidden aggregation stages (`$out`, `$merge`) and operators (`$where`, `$function`, `$accumulator`)
- Field projection, sort, limit, skip, and string truncation for readable output
- Configurable query timeouts

## Install

Requires Go 1.21+.

```sh
make build       # produces ./mongoquery
make install     # copies binary to $HOME/.local/bin (override with PREFIX=...)
```

Make sure `$HOME/.local/bin` is on your `PATH`.

## Configuration

Create `~/.mongoquery.yaml` (or point `MONGOQUERY_CONFIG` at another path):

```sh
make config      # seeds ~/.mongoquery.yaml from config.example.yaml
```

Example:

```yaml
environments:
  staging:
    services:
      orders:
        uri: mongodb+srv://readonly:pass@staging-orders.example.net/orders_db
      billing:
        uri: mongodb+srv://readonly:pass@staging-billing.example.net/billing_db
  production:
    services:
      orders:
        uri: mongodb+srv://readonly:pass@prod-orders.example.net/orders_db
        database: orders_db    # optional, overrides db parsed from URI
```

Use connection strings whose user has the `read` role only.

## Usage

Show configured environments, services, and help:

```sh
mongoquery
```

### List collections

```sh
mongoquery collections --env <env> --service <svc>
```

### Find documents

```sh
mongoquery find --env <env> --service <svc> --collection <c> [flags]
```

Flags:

| Flag | Description |
| --- | --- |
| `--query JSON` | filter (default `{}`) |
| `--projection JSON` | e.g. `{"_id":0,"email":1}` |
| `--sort JSON` | e.g. `{"createdAt":-1}` |
| `--limit N` | default 20 |
| `--skip N` | default 0 |
| `--fields a,b,c` | restrict displayed top-level fields |
| `--full` | disable string truncation |
| `--timeout N` | seconds, default 30 |

Examples:

```sh
mongoquery find --env staging --service orders --collection users \
  --query '{"email":"x@y.com"}'

mongoquery find --env production --service billing --collection invoices \
  --sort '{"createdAt":-1}' --limit 5
```

### Aggregate

```sh
mongoquery aggregate --env <env> --service <svc> --collection <c> --pipeline JSON [flags]
```

Flags:

| Flag | Description |
| --- | --- |
| `--pipeline JSON` | aggregation array (required) |
| `--limit N` | soft cap on results (default 100) |
| `--fields a,b,c` | restrict displayed top-level fields |
| `--full` | disable string truncation |
| `--timeout N` | seconds, default 60 |

Forbidden stages: `$out`, `$merge`. Forbidden operators: `$where`, `$function`, `$accumulator`.

Example:

```sh
mongoquery aggregate --env staging --service orders --collection orders \
  --pipeline '[{"$match":{"status":"paid"}},{"$group":{"_id":"$userId","n":{"$sum":1}}}]'
```

### Per-subcommand help

```sh
mongoquery find --help
mongoquery aggregate --help
mongoquery collections --help
```

## Safety

`mongoquery` validates filters and pipelines before sending them to MongoDB and refuses any stage or operator that would write data or execute server-side JavaScript. Pair this with a read-only database user for defense in depth.

## Development

```sh
make fmt         # go fmt
make vet         # go vet
make test        # go test
make tidy        # go mod tidy
make clean       # remove built binary
```
