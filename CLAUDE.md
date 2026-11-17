# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

- `make build` — compile `./mongoquery` (Go 1.25+).
- `make install` — install to `$HOME/.local/bin` (override with `PREFIX=...`).
- `make run ARGS='find --env staging ...'` — build and run with arguments.
- `make fmt` / `make vet` / `make test` / `make tidy` — standard Go workflow targets.
- `go test ./... -run TestName` — run a single test.
- `make config` — seed `~/.mongoquery.yaml` from `config.example.yaml`. `MONGOQUERY_CONFIG` overrides the path.

## Architecture

Single-package Go CLI (`package main`) for read-only MongoDB inspection. All source files sit at the repo root — no subpackages.

Request flow per invocation:
1. `main.go` dispatches `os.Args[1:]` on the first positional token to a `cmd*` handler. Subcommands beyond the README: `home`, `list`, `count`, `distinct` (see `main.go` switch for the full set).
2. Handlers in `commands.go` parse their own `flag.FlagSet`, call `loadConfig()` (`config.go`), then `connect()` to resolve env+service → `*mongo.Database` using `SecondaryPreferred` read preference. The database name comes from `ServiceConfig.Database` or is parsed from the URI.
3. Filters/projections/pipelines are decoded into `bson.D`/`bson.A` and passed through `safety.go` (`validateFilter` / `scanForbidden` / `validatePipeline`) **before** being sent to Mongo. This is the load-bearing safety layer — any new query-shaped subcommand must run user input through `scanForbidden` (rejects `$where`, `$function`, `$accumulator`) and, for aggregations, the stage-level check for `$out`/`$merge`.
4. Results are rendered via helpers in `output.go` (field projection, string truncation, pretty-printing). `home.go` renders the no-arg landing screen listing configured envs/services.

Database commands support `--format json` for canonical Extended JSON with no truncation or hints. Errors go to stderr. Find only runs an exact matching count when `--count` is requested.

Config model (`config.go`): two-level map `environments[env].services[service] → {uri, database?}`. Resolution errors list configured keys via `joinKeys` to make typos obvious.

## Safety contract

The tool's value proposition is *read-only by construction*. Two layers, both must hold:
- Operator/stage validation in `safety.go` (in-process).
- Mongo connection strings must use users with the `read` role only (operational, enforced outside the binary).

When adding features, do not introduce code paths that bypass `validateFilter`/`validatePipeline`, and do not add write-capable Mongo operations (`InsertOne`, `UpdateMany`, etc.) — the binary intentionally has none.
