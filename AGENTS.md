# Repository Guidelines

## Project Structure & Module Organization

- `cmd/kongweave/`: CLI argument handling, help, and output channels.
- `internal/weave/`: YAML parsing, overlay composition, resource identities, tag handling, provenance, and bundle publication. Keep shared composition separate from decK/kongctl identity rules.
- Tests live beside Go source in `*_test.go` files.
- `examples/deck/` and `examples/kongctl/`: native bases, dev/prod overlays, patches, and referenced assets.
- `docs/`: design decisions and verification results. `.github/workflows/ci.yml` defines CI.
- `bin/` and `dist/`: ignored local binaries and generated bundles.

## Build, Test, and Development Commands

Use Go 1.23 or later from the repository root.

- `make build`: compile the standalone CLI to `bin/kongweave`.
- `make test`: run `go test ./...`, including sample builds.
- `make check`: run `go vet ./...` and `go test -race ./...`.
- `gofmt -w cmd internal`: format Go source.
- `scripts/verify-offline.sh`: optionally verify samples with installed decK and kongctl; kongctl lint checks example rules, not full native validation.

```sh
bin/kongweave build examples/deck/overlays/dev -o dist/review-dev
bin/kongweave explain examples/deck/overlays/prod \
  --kind plugins --name rate-limiting --scope route=catalog-public \
  --field /config/minute
```

Choose a new output directory for each build; existing bundles are never overwritten.

## Coding Style & Naming Conventions

Follow idiomatic Go and `gofmt` indentation. Use descriptive names, lowercase package names, and behavior-oriented `Test...` names. Format YAML examples with two-space indentation. Keep CLI messages and documentation in English. Comments should explain non-obvious compatibility or safety decisions, rather than restating code.

## Testing Guidelines

Use Go's standard `testing` package. Add regression tests for observable semantics: partial merges, scope matching, cardinality failures, null/array distinctions, tags, file relocation, history, and failed publication. Keep tests offline and use temporary directories. No numerical coverage threshold is configured. Run `make check` before submitting code changes.

## Commit & Pull Request Guidelines

No Git history is available in this checkout, so no established commit convention can be inferred. Use concise imperative subjects, such as `Fix plugin scope matching`. PRs should describe the problem, resulting behavior, relevant issues, verification commands, and remaining limitations. Include sample YAML when syntax changes, and update README/design documentation with contract changes.

## Configuration & Safety

Preserve native YAML types and deferred tags. Never resolve environment values or secrets during composition. Use fictional sample data, keep values out of diagnostics, and reject unsupported structures explicitly. Native deployment commands are outside offline verification.
