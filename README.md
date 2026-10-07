# Kongweave

[English](README.md) | [日本語](README_ja.md)

**Environment overlays for decK and kongctl.**

Kongweave is an offline Go CLI that composes a native Kong configuration with ordered environment patches.
Keep one base, change only the fields that differ, and address resources by stable identifiers instead of array positions or UUIDs.

It produces native YAML and a portable directory of referenced files.
It never connects to Kong or Konnect, executes templates, reads environment values, or invokes another CLI.
This is an independent, early MVP, not an official Kong tool.

Read the [user guide](docs/guide.md) for a complete workflow, configuration reference, CLI usage, and troubleshooting.

## Why Kongweave?

Environment differences such as hosts and rate limits can already be applied with [decK `file patch`](https://developer.konghq.com/deck/file/manipulation/patch/), using selectors and multiple patch files.
If a fixed set of patch commands meets your needs, you may not need another tool.

Kongweave is useful when a team wants environment definitions, target checks, and explanations of the generated configuration to follow a shared contract in the repository:

- **Declare how each environment is assembled.** An overlay identifies its base or parent overlay and lists its patches in order, so reviewers can follow inheritance and precedence from the configuration files.
- **Make target expectations explicit.** Select resources by `name` or `ref` and scope; updates and removals require exactly one match, while adding a resource requires it to be absent.
- **Apply partial changes with defined merge rules.** Update `config.minute` while preserving `config.policy`; ordinary arrays are replaced as a whole, and `remove` explicitly deletes a field or resource.
- **Trace a generated setting to its sources.** `kongweave explain` reports the base and patch files that contributed to a resource or field, in application order, without displaying configuration values.

Scripts and CI checks around patch commands can implement similar conventions.
Kongweave packages these rules and composition history into one offline CLI for native decK and kongctl YAML, reducing the custom orchestration each project needs to maintain.
Git tracks changes to source files over time; `explain` describes how the current inputs were composed.
The same inputs produce reproducible bundles for native validation and deployment with decK or kongctl.

## Installation

Download a prebuilt archive from [GitHub Releases](https://github.com/shukawam/kongweave/releases); Go, decK, and kongctl are not required.
Run the commands in an empty working directory.
Downloads become available after the first release is published.

### macOS

The example below is for Apple Silicon; on Intel Macs, change `darwin_arm64` to `darwin_amd64`.

```sh
(
  set -eu
  repository="shukawam/kongweave"
  archive="kongweave_darwin_arm64.tar.gz"
  url="https://github.com/$repository/releases/latest/download"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  grep -F "  $archive" checksums.txt | shasum -a 256 -c -
  tar -xzf "$archive"
  sudo install -d /usr/local/bin
  sudo install -m 755 kongweave /usr/local/bin/kongweave
  kongweave version
)
```

### Linux

The example below is for x86_64; on ARM64, change `linux_amd64` to `linux_arm64`.

```sh
(
  set -eu
  repository="shukawam/kongweave"
  archive="kongweave_linux_amd64.tar.gz"
  url="https://github.com/$repository/releases/latest/download"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  grep -F "  $archive" checksums.txt | sha256sum -c -
  tar -xzf "$archive"
  sudo install -d /usr/local/bin
  sudo install -m 755 kongweave /usr/local/bin/kongweave
  kongweave version
)
```

The commands verify the archive's SHA-256 before extracting and installing it.
Archives include the documentation and examples as well as the standalone binary.
See the [installation guide](docs/guide.md#install-and-build-the-examples) for version pinning, upgrades, and installation without sudo.

## Quick start

After extracting the archive, run these commands from the extracted directory to use the included examples:

```sh
kongweave build examples/deck/overlays/dev --output dist/deck-dev
kongweave build examples/deck/overlays/prod --output dist/deck-prod
kongweave build examples/kongctl/overlays/dev --output dist/kongctl-dev
kongweave build examples/kongctl/overlays/prod --output dist/kongctl-prod

kongweave explain examples/deck/overlays/prod \
  --kind plugins --name rate-limiting --scope route=catalog-public \
  --field /config/minute
kongweave explain examples/kongctl/overlays/prod \
  --kind ai_gateway_models --ref example-chat --scope ai_gateway=example-ai \
  --field /config/route/paths --json
```

Each build requires a **new output directory**.
For another build, choose a new path.
Existing output, including an empty directory or symlink, is refused.
The default is `<overlay-directory>/build`; explicit relative `--output` paths are relative to the caller.
Input references always resolve from the declaring file, independently of the caller's working directory.

A bundle contains:

```text
dist/kongctl-dev/
  config.yaml       # Native YAML, with deferred tags intact
  manifest.json     # Ordered composition history; no configuration values
  assets/           # Referenced files, copied without evaluation
```

Move the entire bundle together.
`config.yaml` is the native tool's entry point; `manifest.json` is Kongweave metadata, not an input for decK or kongctl.

## Base and overlays

```text
base/kong.yaml
overlays/common/overlay.yaml
overlays/dev/overlay.yaml
overlays/dev/patches/environment.yaml
```

The base is normal decK YAML:

```yaml
_format_version: "3.0"
services:
  - name: catalog
    host: catalog.example.com
    plugins:
      - name: rate-limiting
        config:
          minute: 60
          policy: local
```

`overlays/dev/overlay.yaml` declares the format and ordered patch files:

```yaml
apiVersion: kongweave/v1alpha1
format: deck
base:
  file: ../../base/kong.yaml
patches:
  - patches/environment.yaml
  - patches/plugins.yaml
```

Alternatively use `base: {overlay: ../common/overlay.yaml}`.
Exactly one base reference is allowed per overlay.
Every overlay declares its format; mixed formats, remote sources, and cycles (including symlink cycles) fail.
Parent patches run first, then the child's files in list order, then operations in each file in list order.
A patch file may intentionally appear more than once.

`patches/environment.yaml`:

```yaml
patches:
  - op: merge
    target: {kind: services, name: catalog}
    value: {host: catalog.dev.example.com}
  - op: merge
    target:
      kind: plugins
      name: rate-limiting
      scope: {service: catalog}
    value:
      config:
        minute: 600
```

The result retains `config.policy: local`.
Patches use YAML values directly; newly introduced kongctl tags are supported too.

## Patch contract

| Operation | Precondition | Effect |
| --- | --- | --- |
| `merge` | Exactly one existing plain mapping | Recursively merge ordinary maps; preserve unspecified fields |
| `replace` | Exactly one existing target | Replace the entire resource or selected field |
| `add` | Target absent; parent exists | Append a resource or insert a mapping field |
| `remove` | Exactly one existing target | Delete the resource or mapping field; `value` is forbidden |

For resource operations, `value` is a resource mapping.
For field operations, `field` is a JSON Pointer relative to the selected resource.
Only mapping-key traversal is supported: `/config/minute` and escaped keys such as `/labels/a~1b`.
There are no array indexes, wildcards, JSONPath expressions, or implicit parent creation.
`merge` of a field requires both existing and new values to be plain maps.
`replace` and `add` can write any YAML value, including `null`.

```yaml
patches:
  - op: remove
    target: {kind: plugins, name: rate-limiting, scope: {service: catalog}}
    field: /config/policy
  - op: add
    target: {kind: plugins, name: cors, scope: {service: catalog}}
    value: {name: cors, config: {origins: [https://app.example.com]}}
  - op: replace
    target: {kind: services, name: catalog}
    field: /host
    value: catalog.prod.example.com
```

* Scalars overwrite; absent keys retain their existing values.
* Explicit `null` is a value, never an implicit delete. `[]` is an empty array, distinct from `null` and an omitted field. Native schemas may prohibit null at particular fields; Kongweave does not make it a valid native value there.
* Ordinary arrays are replaced in full, without sorting. An element having `name` does not make it a resource. AI model `targets`, policy reference lists, plugin configuration arrays, and route paths are ordinary arrays.
* Known resource collections cannot be merged or selected through `field`. Address individual resources. Whole-resource `replace` can explicitly replace its children; `remove` also removes its nested children. Empty resource arrays remain empty after removing their last member.
* Identities and scopes are immutable. Use remove followed by add to rename or move a resource. Native relationships are not automatically rewritten.
* Scope in values follows the operation. `add` with a single scope nests the resource under that parent and writes no scope field; `add` with a combined plugin scope appends at the root and fills in any binding field the value omits, in fixed key order. `replace` supplies the whole resource, so a flat resource's binding fields (`service`, `api`, ...) must be restated or the operation fails as an identity change. Nested resources take their scope from the parent, so nested `replace` values need no scope field.
* `field` is an RFC 6901 JSON Pointer: `/` and `/a/` name the empty-string key, which is usually absent and therefore a cardinality error.
* Tagged mappings such as `!secret` are atomic expressions. Supplying one replaces that expression; its internal fields cannot be patched with a field pointer.
* Targets are singular. No match is an error, never a merge-upsert. Multiple matches fail. Cardinality errors include the patch file and line, target, expected count, and actual count. Duplicate identities fail before patching and after every operation, even if a later patch would remove the duplicate.

## Supported input and resource identities

Input files contain exactly one YAML mapping document.
Native resource schemas are **not** fully validated by Kongweave; use the official tools too.

| Format | `kind` | Identity and supported placement |
| --- | --- | --- |
| decK 3.0 | `services` | `name`, root |
| decK 3.0 | `routes` | `name`, root or `services[].routes`; optional `scope.service` |
| decK 3.0 | `consumers` | `name` selector matches native `username`, root |
| decK 3.0 | `plugins` | `name`, optional `instance_name`, exact service/route/consumer scope; root or nested under those resources |
| kongctl | `portals`, `apis`, `control_planes`, `application_auth_strategies`, `ai_gateways` | Explicit `ref`, root |
| kongctl | `portal_pages` | `ref`, root with `portal`, or `portals[].pages`; `scope.portal` |
| kongctl | `api_versions`, `api_documents`, `api_publications` | `ref`, root with `api`, or `apis[].versions/documents/publications`; `scope.api` |
| kongctl | `ai_gateway_models`, `ai_gateway_model_providers`, `ai_gateway_policies` | `ref`, root with `ai_gateway`, or `ai_gateways[].models/model_providers/policies`; `scope.ai_gateway` |

The examples follow the [kongctl declarative resource reference](https://github.com/Kong/kongctl/blob/main/docs/declarative-resource-reference.md).
AI model endpoint, model target, and routing differences are demonstrated using `targets[].config.upstream_url`, `targets[].name`, and `config.route`.

Omitted `scope` searches all scopes and still requires exactly one match.
Supplied `scope` must match **all** associations exactly.
`scope: {}` explicitly selects a global/unscoped resource (`--global` for explain).
A plugin under a Route has route scope; the Route's Service is not an additional plugin binding.
Combined bindings use, for example, `scope: {service: catalog, consumer: client}`.
Plugin addition always requires explicit scope.
Single-scope additions are nested under the parent; combined-scope plugins are added at the root.

All supported decK services/routes/consumers need stable names/usernames.
Flat relationships accept strings or `{name: ...}` (`{username: ...}` for Consumers).
UUID-only relationships and Consumer Group plugin scopes are unsupported.
Service and Route names are globally unique within their respective kinds; plugin identity includes its scope and optional `instance_name`.
Nonempty plugin instance names must also be globally unique.
Kongctl refs must be globally unique across supported resource kinds.
Flat kongctl child parent selectors accept a literal ref or scalar `!ref`; parents must be declared in the same base.

## Native YAML and limits

Kongweave uses YAML nodes, preserving scalar types and unevaluated `!ref`, `!file`, `!env`, `!secret`, `!lookup`, and `!external` expressions.
It checks tag shapes and supported nesting, not every native field's eligibility for a tag.
Relationship lookups in data fields remain deferred; lookup-based *parent scopes* cannot be used for composition.
See [the native tag contract](https://github.com/Kong/kongctl/blob/main/docs/declarative.md#yaml-tags).

`!file` scalar paths (including `#extract`) and `{path, extract}` forms are rebased to copied assets.
`_deck.files` is handled only on control planes.
Paths introduced by a patch resolve from that **patch file**, while unchanged paths retain their base-file origin.
Assets must be local regular files within the declaring file's directory, including after symlink resolution.
Globs, URLs, absolute paths, dynamic paths, stdin, and escaping paths are rejected.
Move shared assets into the declaring directory if necessary.
`_deck` assets must be single-document decK 3.0 YAML without custom tags.
Arbitrary strings that happen to look like paths are not rewritten.
Nonempty `_deck.flags` are rejected because they can introduce additional file paths that cannot safely be rebased.

File contents are copied as opaque bytes, including a deferred `!secret {source: !file ...}` source; they are never interpolated or extracted.
Such a bundle **contains those files**, so use public fixtures or treat it as sensitive.
Files are created with mode `0600` and directories with `0700` on Unix.
Only surviving references are read.
There is a 10 MiB limit per input/asset, 100-level YAML nesting limit, and 64-level overlay inheritance limit.

One native base per inheritance chain preserves the file boundary for `_defaults`, which is retained unchanged.
Added resources use that base's file context.
`_templates` and `_extends` are rejected rather than expanded or combined; inherited identities and resources would otherwise make matching and provenance ambiguous.
YAML anchors, aliases, merge keys, non-string mapping keys, multiple documents, unlisted root resource types, and recognized unsupported child structures are rejected.
Comments and formatting are not preserved.

There is no network I/O, environment/secret lookup, conversion between formats, remote base download, deployment, arbitrary code evaluation, state management, or drift detection.
Native plan/apply/sync and their validation remain the responsibility of decK and kongctl.

## Explain and reproducibility

`explain` replays composition and reports sources, lines, operations, resource identities, and written field paths, in order.
It includes overwritten changes, explicit deletions, and children removed/replaced by whole-resource operations.
Select child resources directly; a parent report does not aggregate child edits.
A field query includes writes to its ancestors (for example, whole-array replacement) but excludes merges that only touch its siblings.
It does not show values, evaluate tags, expand defaults, or predict native template/planner behavior.
Resource identifiers, field names, and source paths are visible, so do not put credentials in those metadata fields.

The same source tree and bytes produce identical config, assets, and manifest, including when the tree or output directory is relocated.
Asset filenames use content hashes; source paths in history are relative to the entry overlay.
Map insertion order and sequence order are retained.
Builds publish a completed staging directory with one rename.
Existing bundles are immutable; a failed build leaves them unchanged.
This is atomic publication, not a power-loss persistence guarantee.
A process crash can leave a staging directory or sibling `.kongweave-lock` file; remove those only after confirming no build is running.

## Hand off to official tools

Offline decK checks, one environment at a time:

```sh
deck file validate --analytics=false dist/deck-dev/config.yaml
deck file validate --analytics=false dist/deck-prod/config.yaml
```

Offline kongctl checks with the included **example rules**, not full schema or remote validation:

```sh
kongctl --no-telemetry lint -f dist/kongctl-dev/config.yaml -r examples/kongctl/ruleset.yaml
kongctl --no-telemetry lint -f dist/kongctl-prod/config.yaml -r examples/kongctl/ruleset.yaml
```

For a later, separately authorized deployment, pass `config.yaml` explicitly to the native workflow.
For example, `deck gateway diff dist/deck-prod/config.yaml` and `kongctl plan -f dist/kongctl-prod/config.yaml --output-file plan.json`.
These commands require the appropriate target, authentication, and connectivity; they are **not offline validation** and are not run by Kongweave.
Do not load the whole bundle recursively: that would also discover assets as configurations.
The examples use fictional hosts and cannot serve traffic without adaptation.
No token is needed to build; the AI example's deferred token is for a later native execution only.

## Development

These contributor checks require a source checkout and Go; they are separate from installing the prebuilt binary.

```sh
go test ./...
go test -race ./...
go vet ./...
```

CI builds the CLI and exercises the semantic tests and both example families.
Run `scripts/verify-offline.sh` for an optional local end-to-end check with decK and kongctl installed.
See [validation and handoff](docs/guide.md#validation-and-handoff) for reproducible checks and their coverage.

Licensed under [Apache-2.0](LICENSE).
