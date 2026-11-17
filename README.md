# mongoquery

A read-only MongoDB query CLI for inspecting environments and services from the terminal. Built for safe ad-hoc investigation — no GUI, no write risk, just fast access to your data.

Filters and pipelines are validated before they reach MongoDB, rejecting any operator or stage that writes data or executes server-side code. Pair it with a `read`-only database user for defense in depth.

## Features

- **Six subcommands:** `find`, `aggregate`, `count`, `distinct`, `collections`, `list`
- **Multi-environment config:** define named environments and services in a single YAML file
- **Read-only by construction:** blocks `$out`, `$merge`, `$where`, `$function`, and `$accumulator` before the query is sent
- **Readable output:** field projection, sort, limit, skip, and string truncation
- **Configurable timeouts** per query

## Install

### From source (requires Go 1.27.4+)

```sh
git clone https://github.com/jlimas/mongoquery.git
cd mongoquery
make install     # installs to $HOME/.local/bin (override with PREFIX=...)
```

Make sure `$HOME/.local/bin` is on your `PATH`.

### Build only

```sh
make build       # produces ./mongoquery binary in the current directory
```

## Configuration

Create `~/.mongoquery.yaml` (or point the `MONGOQUERY_CONFIG` environment variable at another path):

```sh
make config      # seeds ~/.mongoquery.yaml from config.example.yaml
```

Config structure:

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
        database: orders_db    # optional — overrides the database parsed from the URI
```

> **Important:** always use connection strings whose user has the `read` role only.

`make config` respects `MONGOQUERY_CONFIG`, creates the file with owner-only
permissions (`0600`), and leaves existing files untouched. Keep credentials out of
version control. Configuration rejects unknown keys, empty environment maps, and
multiple YAML documents so typos do not silently change the selected target.

Run `mongoquery` with no arguments to see all configured environments and services.

## Usage

### List configured environments and services

```sh
mongoquery list
```

### List collections

```sh
mongoquery collections --env <env> --service <svc>
```

### Find documents

```sh
mongoquery find --env <env> --service <svc> --collection <c> [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--query JSON` | `{}` | filter in MongoDB Extended JSON |
| `--projection JSON` | — | e.g. `{"_id":0,"email":1}` |
| `--sort JSON` | — | e.g. `{"createdAt":-1}` |
| `--limit N` | `20` | max documents to return |
| `--count` | `false` | include exact matching total; may scan many documents |
| `--skip N` | `0` | documents to skip |
| `--fields a,b,c` | — | restrict displayed top-level fields |
| `--full` | — | disable string truncation |
| `--timeout N` | `30` | query timeout in seconds |

```sh
mongoquery find --env staging --service orders --collection users \
  --query '{"email":"x@example.com"}'

mongoquery find --env production --service billing --collection invoices \
  --sort '{"createdAt":-1}' --limit 5 --fields _id,amount,status
```

### Count documents

```sh
mongoquery count --env <env> --service <svc> --collection <c> [--query JSON]
```

```sh
mongoquery count --env staging --service orders --collection users \
  --query '{"active":true}'
```

### Distinct values

```sh
mongoquery distinct --env <env> --service <svc> --collection <c> --field <field> [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--field` | (required) | field to compute distinct values for |
| `--query JSON` | `{}` | filter to apply before computing distinct values |
| `--limit N` | `100` | max values to display |
| `--timeout N` | `30` | query timeout in seconds |

```sh
mongoquery distinct --env staging --service orders --collection orders --field status
```

`--limit` only caps displayed distinct values: MongoDB still computes and returns
the complete set, which can be expensive or exceed its BSON response size limit.
For high-cardinality fields, use an aggregation with `$group` followed by `$limit`
to bound returned results (grouping itself can still scan the full input).

### Aggregate

```sh
mongoquery aggregate --env <env> --service <svc> --collection <c> --pipeline JSON [flags]
```

| Flag | Default | Description |
| --- | --- | --- |
| `--pipeline JSON` | (required) | aggregation pipeline array |
| `--limit N` | `100` | soft cap on results |
| `--fields a,b,c` | — | restrict displayed top-level fields |
| `--full` | — | disable string truncation |
| `--timeout N` | `60` | query timeout in seconds |

Limits and timeouts must be positive; skip must be nonnegative. Aggregation
`--limit` caps client-side output, not the work required by the pipeline. Put
appropriate `$match`, `$project`, and `$limit` stages in the pipeline to reduce
server work where semantics allow. Find queries do not compute an exact total
unless `--count` is supplied.

```sh
mongoquery aggregate --env staging --service orders --collection orders \
  --pipeline '[{"$match":{"status":"paid"}},{"$group":{"_id":"$userId","n":{"$sum":1}}}]'
```

### Per-subcommand help

```sh
mongoquery find --help
mongoquery aggregate --help
mongoquery count --help
mongoquery distinct --help
mongoquery collections --help
```

### Machine-readable output

All database commands (`find`, `aggregate`, `count`, `distinct`, `collections`)
accept `--format json`. This emits canonical MongoDB Extended JSON, preserving
ObjectIDs, numeric types, dates, and decimals without string truncation or help
text. `--fields` still restricts displayed fields for find and aggregate.

```sh
mongoquery find --env staging --service orders --collection users \
  --format json > users.json
```

Find and aggregate return arrays of documents; distinct returns an array of
values; collections returns an array of names. With `find --count`, the result
is an object containing `results` and `total`. Count returns an object containing
`collection` and `matched`. Empty result arrays are `[]`, not `null`.

Errors go to stderr and return a nonzero exit status. Subcommand `--help`
succeeds without requiring configuration. Unknown positional arguments are
rejected; supply all query options as flags.

## Safety

`mongoquery` enforces read-only behavior at two layers:

1. **In-process validation** — filters and pipelines are scanned before being sent to MongoDB. The following are rejected outright:
   - Aggregation stages: `$out`, `$merge`
   - Operators in filters, projections, and pipelines: `$where`, `$function`, `$accumulator`
2. **Connection-level enforcement** — use a database user with the `read` role only. The binary has no write operations (`InsertOne`, `UpdateMany`, etc.) and never will.

## Development

```sh
make fmt         # go fmt
make vet         # go vet
make test        # go test
make vuln        # govulncheck (version managed in go.mod)
make tidy        # go mod tidy
make clean       # remove built binary
```

CI uses the Go version in `go.mod` and runs formatting, vet, race tests, builds,
and vulnerability scanning. Dependabot checks Go modules (including development
tools) and GitHub Actions weekly.
