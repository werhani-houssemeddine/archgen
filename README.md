# archgen

A code generator for **Go modular monoliths built with Clean Architecture** on top of the [archkit](https://github.com/werhani-houssemeddine/go-archkit) Go library.

It creates projects, modules, aggregates with their complete CRUD slice (domain → use cases → persistence → HTTP → migration → tests), use cases, events, cross-module contracts, ports and migrations. Its `check` command then enforces the architecture rules in CI.

Every generator writes code that compiles, passes `go vet`, and comes with passing tests. Generated projects have been run end-to-end against PostgreSQL.

```bash
go install github.com/werhani-houssemeddine/archgen/cmd/archgen@latest     # or: make install
```

archgen has no third-party dependencies (Go 1.24+).

## Quick start

```bash
archgen new backend --module github.com/acme/backend --kit github.com/werhani-houssemeddine/go-archkit
cd backend && go get github.com/werhani-houssemeddine/go-archkit@latest && go mod tidy

archgen module users
archgen module courses
archgen resource courses Course --fields "title:string:unique, summary:text:optional, level:enum(beginner|advanced):index, price:float, published:bool"

cp .env.example .env && make db-up && make run
curl -X POST localhost:8080/api/v1/courses -H "Authorization: Bearer $TOKEN" \
     -d '{"title":"Go","level":"beginner","price":10}'
```

If archkit isn't published yet, point the project at a local checkout: `archgen new backend --kit-replace ../archkit` adds a `replace` directive to `go.mod`.

## Commands

| Command | Generates |
|---|---|
| `new <name> [--module] [--kit] [--kit-replace] [--driver pgx\|pq] [--migrations timestamp\|sequential]` | Project skeleton: `cmd/api`, `internal/app` (bootstrap, config, dependencies, router, RBAC), `internal/shared/{kernel,database,http,logging,observability}`, embedded migrations with the outbox table, Makefile, docker-compose, `.env.example`, README |
| `module <name>` | `internal/modules/<name>/{domain,application/{commands,queries,ports},infrastructure/persistence,interfaces/http,contract}`, `module.go` (Deps/New/RegisterRoutes/Subscribe), a migration creating the module's **own schema**, and wiring in `internal/app/dependencies.go` |
| `resource <module> <Name> --fields "…"` | Aggregate with private fields, invariants, events and snapshot/restore; errors catalog; repository port with filter and sorting; Create/Update/Delete commands; Get/List queries with a view model; PostgreSQL repository (optimistic locking, outbox, unique-constraint → domain-error mapping); in-memory repository; HTTP endpoints `POST/GET/GET{id}/PUT/DELETE`; migration; domain, application and HTTP tests; module wiring |
| `command <module> <Name> [--fields]` | Write use case (command, validation, permissions, handler, registration) |
| `query <module> <Name> [--fields]` | Read use case (query, result model, handler, registration) |
| `event <module> <Name> [--fields] [--public]` | Domain event (internal), or with `--public` an integration event in `contract/` |
| `subscribe <module> <other>.<Event>` | Idempotent event handler in `application/eventhandlers`, subscribed in `module.go` |
| `contract <module> <Name> [--resource <Aggregate>]` | Public read interface + DTO in `contract/`, adapter in the module, and an accessor on `Module` |
| `use <module> <other>.<Contract>` | Adds the contract to the consumer's `Deps` and wires `ms.Other.Contract()` in the composition root, building the provider first |
| `port <module> <Name> [--adapter <dir>]` | Interface in `application/ports` + adapter stub in `infrastructure/<dir>` |
| `migration <module\|-> <name>` | Empty, timestamped SQL migration |
| `check [--tests] [--json]` | Architecture lint; exit code 1 on violations (use it in CI) |
| `list` | Modules with their aggregates, commands, queries, ports, events, contracts and subscribers |

Common flags: `--dir <project>`, `--dry-run` (show the plan only), `--force` (overwrite existing files).

### Field DSL

`name:type[:modifier…]`, comma-separated.

| Types | Go | PostgreSQL | Validation |
|---|---|---|---|
| `string` | `string` | `text` | required, max 255 |
| `text` | `string` | `text` | required, max 10 000 |
| `email` | `string` | `text` | required, email |
| `url` | `string` | `text` | required, http(s) URL |
| `uuid` | `string` | `uuid` | required, UUID |
| `enum(a\|b\|c)` | `string` | `text CHECK (… IN …)` | required, one of |
| `int`, `int64`, `float`, `bool`, `time` | `int`, `int64`, `float64`, `bool`, `time.Time` | `integer`, `bigint`, `double precision`, `boolean`, `timestamptz` | `time` is required |

Modifiers:
- `optional` makes the field a pointer type, nullable, and not required.
- `unique` adds a named constraint and a domain error such as `COURSE_TITLE_TAKEN` → 409.
- `index` adds an index.

## How the generated code maps to your architecture document

- **Domain** imports only archkit core (`domain`, `errs`, `validate`) and `shared/kernel`. Aggregates keep their fields unexported, enforce invariants in `New…`/`Update`, and record domain events. Persistence goes through `Snapshot()`/`Restore…()`, so entities never become DB models.
- **Application:** each use case is a command or query with a handler struct holding small interfaces. Permissions are declared on the request, and validation, authorization and transactions are applied by the mediator pipeline.
- **Infrastructure** implements the domain ports: a PostgreSQL repository in the module's schema, with outbox events committed in the same transaction.
- **Interfaces:** HTTP request DTOs map to commands and queries, so HTTP types never reach the application layer.
- **Boundaries:** other modules only see `contract/` (read interfaces, DTOs, public events), wired explicitly with `archgen use`. Each module owns a PostgreSQL schema.
- **Composition root:** `internal/app` builds the platform (DB, outbox, bus, mediator, JWT, RBAC) and the modules. Modules never instantiate infrastructure themselves.

### `archgen check` rules

| Rule | Meaning |
|---|---|
| `layer-direction` | domain ↛ application/infrastructure/interfaces/contract; application ↛ infrastructure/interfaces; interfaces ↛ infrastructure; infrastructure ↛ interfaces; contract ↛ any internal layer |
| `layer-technology` | domain: no `net/http`, `database/sql`, drivers, ORMs, brokers, archkit infrastructure; application: no HTTP/SQL/drivers; interfaces: no SQL |
| `module-boundary` | a module imports another module **only** through `<module>/contract` |
| `domain-isolation` | the domain never imports other modules, not even their contracts |
| `shared-no-business` | `internal/shared` never imports business modules |

`cmd/` and `internal/app` (the composition root) are exempt. `_test.go` files are skipped unless you pass `--tests`.

## Safety

- Existing files are **never overwritten** without `--force`.
- Wiring is inserted at `// archgen:<marker>` comments, at most once. If you delete a marker, archgen prints the snippet for you to add by hand.
- Generated Go is gofmt-validated before writing, and unused imports are pruned.
- `--dry-run` shows exactly what would change.

## Development

```bash
make test                                   # unit tests + generate a project and run `check`
ARCHKIT_PATH=/path/to/archkit make test     # also `go mod tidy && go vet && go test` the generated project
```
