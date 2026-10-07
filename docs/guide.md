# Kongweave user guide

[Documentation](README.md) | [日本語](guide_ja.md)

Kongweave generates native decK or kongctl YAML from one shared base and an ordered chain of environment overlays.
It performs composition offline; it does not connect to Kong or Konnect, evaluate secrets, expand native templates, or deploy the result.
The two input formats remain separate throughout the workflow.

## Contents

1. [Install and build the examples](#install-and-build-the-examples)
2. [Create an environment overlay](#create-an-environment-overlay)
3. [Overlay and patch reference](#overlay-and-patch-reference)
4. [Select resources](#select-resources)
5. [Native YAML and referenced files](#native-yaml-and-referenced-files)
6. [CLI reference](#cli-reference)
7. [Explain changes](#explain-changes)
8. [Bundles and reproducibility](#bundles-and-reproducibility)
9. [Validation and handoff](#validation-and-handoff)
10. [Troubleshooting](#troubleshooting)

## Install and build the examples

### Download a release

Prebuilt archives are provided for macOS and Linux on arm64 and amd64.
Go, decK, and kongctl are not required to install or run Kongweave.
Run the following in an empty working directory after the first release is available.
This example detects your platform, verifies the checksum, and installs into your user directory without sudo.

```sh
(
  set -eu
  repository="shukawam/kongweave"
  url="https://github.com/$repository/releases/latest/download"
  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) echo "Unsupported operating system" >&2; exit 1 ;;
  esac
  case "$(uname -m)" in
    arm64|aarch64) arch=arm64 ;;
    x86_64|amd64) arch=amd64 ;;
    *) echo "Unsupported architecture" >&2; exit 1 ;;
  esac
  archive="kongweave_${os}_${arch}.tar.gz"
  curl -fL "$url/$archive" -o "$archive"
  curl -fL "$url/checksums.txt" -o checksums.txt
  if [ "$os" = darwin ]; then
    grep -F "  $archive" checksums.txt | shasum -a 256 -c -
  else
    grep -F "  $archive" checksums.txt | sha256sum -c -
  fi
  tar -xzf "$archive"
  mkdir -p "$HOME/.local/bin"
  install -m 755 kongweave "$HOME/.local/bin/kongweave"
  "$HOME/.local/bin/kongweave" version
)
```

Add the install directory to PATH for the current shell:

```sh
export PATH="$HOME/.local/bin:$PATH"
kongweave version
```

Add that PATH setting to `~/.zshrc` or `~/.bashrc` for future sessions.
For a system-wide installation, use the commands in the [README](../README.md#installation), which install into `/usr/local/bin`.
The archive includes `docs/`, `examples/`, the README files, and the license; keep the extracted directory to try the examples.

### Choose a version or platform

| Platform | Archive |
| --- | --- |
| macOS Apple Silicon | `kongweave_darwin_arm64.tar.gz` |
| macOS Intel | `kongweave_darwin_amd64.tar.gz` |
| Linux x86_64 | `kongweave_linux_amd64.tar.gz` |
| Linux ARM64 | `kongweave_linux_arm64.tar.gz` |

`releases/latest/download` selects the latest stable release.
For a reproducible installation or a prerelease, use a published tag instead:

```sh
url="https://github.com/$repository/releases/download/v0.1.0"
```

Replace `v0.1.0` with the desired published tag in the download recipe, and fetch both the archive and `checksums.txt` from that same release.
Checksums detect mismatched or corrupted downloads; they are not a publisher signature.
Reinstall a chosen release to upgrade or downgrade, then check `kongweave version`.
Uninstall by removing only the executable from the install directory.
Homebrew packages and Windows binaries are not provided by this release workflow.

### Build the examples

Run the commands below from the extracted archive directory:

```sh
kongweave version
kongweave build examples/deck/overlays/dev --output dist/deck-dev
kongweave build examples/deck/overlays/prod --output dist/deck-prod
kongweave build examples/kongctl/overlays/dev --output dist/kongctl-dev
kongweave build examples/kongctl/overlays/prod --output dist/kongctl-prod
```

Each output directory must be new, including on repeated runs.
Inspect `config.yaml` for the generated native configuration and `manifest.json` for the ordered composition history.
Move or archive the entire output directory so its `assets/` references remain valid.
All example hosts and credentials are fictional or deferred; no account or token is needed to build them.

The decK examples change a Service host, timeouts, Route methods, and Plugins in different scopes.
The kongctl examples change API documentation, AI Gateway metadata, model endpoints, routing, and sanitizer policy configuration.
Model `targets` and their `name` fields are ordinary configuration data, so replacing that array requires supplying all targets you want to retain.

## Create an environment overlay

This walkthrough adds a staging environment to the existing decK example.
Create the following two files under `examples/deck/overlays/staging/` without changing the shared base.

```text
examples/deck/
  base/kong.yaml
  overlays/
    common/
      overlay.yaml
      timeouts.yaml
    staging/
      overlay.yaml
      patches/environment.yaml
```

Write `examples/deck/overlays/staging/overlay.yaml`:

```yaml
apiVersion: kongweave/v1alpha1
format: deck
base:
  overlay: ../common/overlay.yaml
patches:
  - patches/environment.yaml
```

Write `examples/deck/overlays/staging/patches/environment.yaml`:

```yaml
patches:
  - op: merge
    target: {kind: services, name: catalog}
    value:
      host: catalog.staging.example.com
      read_timeout: 8000
  - op: merge
    target:
      kind: plugins
      name: rate-limiting
      scope: {route: catalog-public}
    value:
      config:
        minute: 120
  - op: replace
    target: {kind: routes, name: catalog-public}
    field: /methods
    value: [GET, HEAD]
  - op: add
    target: {kind: plugins, name: cors, scope: {service: catalog}}
    value:
      name: cors
      config:
        origins: [https://app.staging.example.com]
  - op: remove
    target: {kind: plugins, name: response-transformer, scope: {}}
```

Build and inspect the staging configuration:

```sh
kongweave build examples/deck/overlays/staging --output dist/deck-staging
kongweave explain examples/deck/overlays/staging --kind plugins --name rate-limiting --scope route=catalog-public --field /config/minute
```

The Service inherits `write_timeout: 10000` from the common overlay and receives the staging host and read timeout.
The Route's rate limit becomes `120`, while its `policy: local` and `limit_by: ip` remain intact.
The Service's separate rate-limiting Plugin is unchanged.
The Route accepts `GET` and `HEAD`, the Service gains a CORS Plugin, and the global response-transformer Plugin is removed.

For kongctl, set `format: kongctl`, reference a native kongctl base, and use `ref` selectors.
The existing [production AI Gateway patch](../examples/kongctl/overlays/prod/patches/environment.yaml) demonstrates changes to `config.route.paths`, the complete `targets` array, and `config.anonymize` on a separately selected policy.
Fields such as `config.route.model` and `config.logging` survive the partial map merge.

## Overlay and patch reference

### Overlay file

The CLI loads `overlay.yaml` inside the directory passed to `build` or `explain`.
An overlay accepts only these keys:

| Key | Required | Meaning |
| --- | --- | --- |
| `apiVersion` | Yes | Exactly `kongweave/v1alpha1` |
| `format` | Yes | `deck` or `kongctl`, consistent across the entire chain |
| `base.file` | One base reference | Relative path to one native YAML file |
| `base.overlay` | Alternative to `base.file` | Relative path to another overlay YAML file, including its filename |
| `patches` | No | Ordered list of relative patch file paths; omission or `[]` applies no local patches |

Use exactly one of `base.file` and `base.overlay`, never both.
All input references resolve relative to the file containing the reference, not the shell's working directory.
For example, a base at `base/kong.yaml` can be referenced from `overlays/dev/overlay.yaml` as `../../base/kong.yaml`.
Local base and patch references may use `..`; copied assets have the stricter directory rule described below.

The native base is loaded first, then ancestor overlays from oldest to nearest, then the selected overlay.
Within each overlay, patch files run in list order, and operations inside each patch file also run in list order.
Listing the same patch file twice applies it twice; an `add` or `remove` may therefore fail on its second application.
Remote references, cycles, mixed formats, and inheritance deeper than 64 levels are rejected.

### Operations

A patch file contains a nonempty `patches` array.
Each entry accepts `op`, `target`, optional `field`, and `value` except for `remove`.
Omitting `field`, or setting it to `""`, selects the whole resource.

| Operation | Whole resource | Field within a resource |
| --- | --- | --- |
| `merge` | Exactly one existing resource; recursively merge a plain mapping | Resource and field must exist uniquely; existing and incoming values must both be plain maps |
| `replace` | Exactly one existing resource; replace its entire mapping, including children | Field must exist; replace it with any YAML value |
| `add` | No matching resource; parent must exist; append a resource mapping | Resource and containing map must exist; field must be absent |
| `remove` | Exactly one existing resource; remove it and its nested children | Field must exist; delete that mapping entry |

`value` is required for `merge`, `replace`, and `add`; explicit `value: null` is present and differs from omitting `value`.
`remove` forbids `value`, even `value: null`.
Only field `add` and `replace` can use a scalar, array, or null as their entire value; resource operations require a plain mapping.
No operation silently creates a missing target or containing map.

### Merge behavior

| Incoming data | Result |
| --- | --- |
| Plain map over plain map | Merge recursively, preserving unspecified keys |
| Scalar | Replace the previous value, preserving its YAML type |
| `null` | Store null; do not delete the key |
| Ordinary array | Replace the entire array without sorting or matching elements |
| `[]` | Store an empty array |
| Empty plain map over a plain map | Retain existing entries |
| Tagged expression | Replace the expression as a unit |
| Omitted key | Retain the existing value |

Known resource arrays such as `services[].plugins` must be edited by selecting individual resources.
They cannot be merged through their parent or traversed using `field`.
Whole-resource `replace` intentionally replaces the resource's complete subtree and can therefore change nested children.
Removing the final member of a resource array leaves `[]` in place.

### Field paths

`field` uses RFC 6901 JSON Pointer syntax, with mapping keys only.
Use `/config/minute` for a nested key, `~1` for a literal `/`, and `~0` for a literal `~` in a key.
For example, `/labels/team~1owner` addresses the key `team/owner` under `labels`.
`/` addresses an empty-string key on the resource; `/config/` addresses an empty-string key within `config`.
These paths still obey the operation's existence requirements.
Array indexes, wildcard selectors, JSONPath, and traversal inside tagged expressions are unsupported.
To change one array element, replace the complete ordinary array with the intended ordered contents.

### Identities and relationships

Resource names, refs, Plugin instance names, and scopes are immutable during `merge` and `replace`.
Adding `instance_name` to a previously unnamed Plugin is also an identity change.
Use explicit `remove` and `add` operations to rename or move a resource, and update other references yourself.
The base and the result of every operation must have valid, unique identities; a later operation cannot repair an invalid intermediate state.
Remove flat children before their parent, or Kongweave will reject their unresolved parent relationship.
Nested children are removed automatically with their parent.

For `add`, a single parent scope nests the new resource under that parent without synthesizing a binding field.
Combined Plugin scopes add the Plugin at the root and fill omitted binding fields in a fixed order.
For `replace`, a flat resource must include its existing binding fields, such as `service` or `api`, in the replacement mapping.
Otherwise the replacement changes its scope and fails.
A nested replacement can omit those fields because its parent determines the scope.

## Select resources

Selectors name native plural resource kinds, independently of where a resource is nested.
The following table lists the supported resource collections; it is not a full schema validator for their native fields.

| Format | Kind | Identity | Supported placement and scope |
| --- | --- | --- | --- |
| decK 3.0 | `services` | `name` | Root |
| decK 3.0 | `routes` | `name` | Root or `services[].routes`; optional `service` scope |
| decK 3.0 | `consumers` | Selector `name` matches `username` | Root |
| decK 3.0 | `plugins` | `name`, optional `instance_name`, bindings | Root or under Services, Routes, or Consumers; `service`, `route`, `consumer` scopes |
| kongctl | `portals`, `apis`, `control_planes`, `application_auth_strategies`, `ai_gateways` | `ref` | Root |
| kongctl | `portal_pages` | `ref` | Root with `portal`, or `portals[].pages`; `portal` scope |
| kongctl | `api_versions`, `api_documents`, `api_publications` | `ref` | Root with `api`, or `apis[].versions/documents/publications`; `api` scope |
| kongctl | `ai_gateway_models`, `ai_gateway_model_providers`, `ai_gateway_policies` | `ref` | Root with `ai_gateway`, or `ai_gateways[].models/model_providers/policies`; `ai_gateway` scope |

Every supported decK Service, Route, and Consumer needs a stable name or username.
Their identifiers are unique across their respective resource kinds, even when Routes are nested under different Services.
Nonempty Plugin instance names must be globally unique.
Kongctl refs must be explicit strings and globally unique across all supported resource kinds.
UUID-only or deferred identities cannot be used as composition targets.

### Plugin scopes

Omitting `scope` searches every scope but still requires one match.
`scope: {}` explicitly selects an unscoped/global resource.
A supplied nonempty scope must match every binding exactly; `{service: catalog}` does not match a Plugin bound to both that Service and a Consumer.

```yaml
target: {kind: plugins, name: rate-limiting, scope: {route: catalog-public}}
```

A Plugin nested under `catalog-public` has Route scope only; the Route's Service is not automatically an additional binding.
For combined bindings, use a scope such as `{service: catalog, consumer: client}` with both parent resources declared.
Use `instance_name` when multiple instances of the same Plugin require further disambiguation.
Omitting `instance_name` searches all instances, so a broad selector can still be ambiguous.
Plugin `add` always requires an explicit scope, including `{}` for a global Plugin.
Consumer Group Plugin bindings are unsupported.

Flat decK bindings accept a stable string, `{name: ...}`, or `{username: ...}` for Consumers.
Flat kongctl child bindings accept a literal ref or a scalar `!ref`; the parent must already be declared locally when the operation is validated.
Kongweave does not resolve remote parent scopes.

## Native YAML and referenced files

Each file must contain exactly one YAML mapping document.
Scalar types, nulls, array order, and supported tags are preserved; comments and formatting are not.
Duplicate mapping keys, non-string keys, YAML merge keys, anchors, and aliases are rejected.

### Deferred kongctl tags

| Tag | Accepted expression shape | Build behavior |
| --- | --- | --- |
| `!ref` | Scalar `ref` or `ref#field` | Keep the reference unevaluated |
| `!file` | Scalar path, optional `#extract`, or `{path, extract}` | Copy the file and rewrite the path; preserve extraction without executing it |
| `!env` | Scalar variable, optional `#extract`, or `{var, extract}` | Preserve without reading the environment |
| `!secret` | Mapping with exactly one of `source` or `parts` | Preserve the expression without resolving it |
| `!lookup`, `!external` | Scalar `field:value` or nonempty selector mapping | Preserve without remote lookup |

`!file` and `!env` mapping forms require a plain string `path` or `var`; optional `extract` must also be a string.
Their payloads cannot contain nested tags.
`!secret.source` must be `!env` or `!file`.
`!secret.parts` must be a nonempty list of plain strings, `!env`, or `!file`, with at least one deferred source.
Lookup mapping values must be strings or direct `!env` expressions, and `id` cannot be combined with other selectors.
Tag syntax checks do not guarantee that a tag is valid at every native field; final field eligibility remains the native tool's responsibility.
Custom tags are unsupported in decK input.

Tagged maps are atomic, so replace an entire secret expression rather than patching its `parts` field.
Patch-introduced tags remain unevaluated just like tags from the base.
The [kongctl base example](../examples/kongctl/base/config.yaml) includes an Authorization value assembled later from `"Bearer "` and `!env KONGWEAVE_DEMO_TOKEN`; builds do not read that variable.

### Portable assets

Base file references resolve relative to the base file, while references introduced by a patch resolve relative to that patch file.
Unchanged references retain their original file context even if other fields on the same resource are modified.
Keep each asset within its declaring file's directory or a descendant, including after resolving symlinks.
Absolute paths, remote URLs, stdin, dynamic paths, globs, and paths escaping that directory are rejected.

For example, `!file docs/catalog.md` in `patches/environment.yaml` requires `patches/docs/catalog.md`.
The [dev kongctl patch](../examples/kongctl/overlays/dev/patches/environment.yaml) and its adjacent `docs/` directory demonstrate this arrangement.
The copied file is stored as `assets/<sha256><extension>`, and the generated reference points there.
Only references surviving composition are copied.
Referenced file contents are opaque bytes, not a second configuration tree to evaluate recursively.

Control Plane `_deck.files` paths are also copied and rewritten.
These assets must be single-document decK 3.0 files without custom tags.
Nonempty `_deck.flags` are unsupported because they can carry additional paths that cannot be relocated reliably.
Other strings that resemble paths are not guessed to be file references.

A deferred secret sourced through `!file` still copies that file's bytes into the bundle.
Use fictional fixtures for public examples and handle real secret-bearing bundles accordingly.
Unix file and directory modes are `0600` and `0700`.
Each input or asset is limited to 10 MiB, and YAML nesting is limited to 100 levels.

### Defaults, templates, and unsupported structures

`_defaults` stays unchanged within the single native base, and added resources inherit that document's context when the native tool processes it.
There is no root-metadata patch operation; use separate bases for different root defaults or patch explicit resource metadata instead.
`_templates` and `_extends` are unsupported and cause an error.
Expand or restructure those inputs before using Kongweave; it does not implement native template evaluation.
Unlisted root resource types and recognized unsupported child structures are rejected, including Consumer credentials, recursive Portal page children, and AI Gateway resource families outside the table above.
Split unsupported configuration into a separate native workflow rather than assuming it passes through untouched.

## CLI reference

Put the overlay directory before command flags.
Use a directory containing `overlay.yaml`, not the filename itself.

| Command or option | Meaning |
| --- | --- |
| `kongweave build <dir>` | Build a new `<dir>/build` bundle |
| `--output <dir>`, `-o <dir>` | Choose a new output directory; relative paths use the caller's working directory |
| `kongweave explain <dir>` | Rebuild the composition in memory and print history; no output bundle is written |
| `--kind <kind>` | Required resource kind for explain |
| `--name <name>` | Required decK name; username for Consumers |
| `--ref <ref>` | Required kongctl ref, used instead of `--name` |
| `--scope key=value` | Exact scope; repeat for combined Plugin bindings |
| `--global` | Explicit empty scope; cannot be combined with `--scope` |
| `--instance-name <name>` | Optional decK Plugin instance selector |
| `--field <pointer>` | Filter history to a field and overlapping writes |
| `--json` | Emit ordered history as JSON |
| `--help`, `-h` | Display help, including `kongweave build --help` |
| `kongweave version` | Print the binary's version string |

`build` writes artifacts to files and its success message to stderr; stdout remains empty.
`explain`, help, and version output go to stdout; errors go to stderr with exit status 1.
Successful commands return status 0.
String flag values remain values even if they look like flags, such as `--name --help` or `--output -h`.

## Explain changes

Use the same identifiers and exact scope as in a patch selector:

```sh
kongweave explain examples/deck/overlays/prod --kind plugins --name rate-limiting --scope route=catalog-public --field /config/minute
kongweave explain examples/kongctl/overlays/prod --kind ai_gateway_models --ref example-chat --scope ai_gateway=example-ai --field /config/route/paths --json
```

Text output identifies each write by order, source path, line number, operation, target, and field paths.
Order numbers are global across the composition, so a filtered report can have gaps.
The base declaration and every explicit write are recorded, including writes of an unchanged value and changes later overwritten or removed.
Whole-resource replacements and removals also record effects on nested child resources.

A field query includes a replacement of any ancestor field or the whole resource.
A merge that changes only a sibling field is excluded.
Select child resources directly; asking about a Service does not aggregate separately targeted Plugin edits.
Deleted resources remain explainable, and selectors must identify one historical resource, including deleted ones.
If multiple historical Plugin instances match, supply the exact scope and instance name.

History excludes configuration values and native work such as defaults, template expansion, secret resolution, or remote lookup.
Resource identifiers, paths, and field names are visible, so keep credentials out of that metadata.
`explain` reads the current source files; it does not load a previous bundle's manifest or validate asset availability through packaging.
Use `build` to check referenced files and inspect a saved bundle's `manifest.json` for that build's history.

## Bundles and reproducibility

| Entry | Purpose |
| --- | --- |
| `config.yaml` | Native tool input, including rewritten file references |
| `assets/` | Referenced bytes; absent when no files are referenced |
| `manifest.json` | Format, config path, file list, and ordered value-free history |

The same source tree and bytes produce the same bundle bytes regardless of the working directory or where the source tree and bundle are relocated.
This does not imply that a later native apply resolves the same environment values or remote resources.
Array order and map insertion order are preserved, and asset names depend on content hashes.
History source paths are relative to the entry overlay.

Builds finish a staging directory before renaming it into the final output path.
Existing files, directories, and symlinks at that path are never overwritten.
Choose a distinct destination for each build, such as a CI job's artifact directory, and archive the whole bundle.
A normal build failure does not expose a partial final directory or change an existing bundle.
An interrupted process can leave staging files or a sibling `.kongweave-lock`; remove them only after checking that no build is active.
Atomic publication does not promise durability through power loss.

## Validation and handoff

Kongweave checks composition rules, selectors, duplicate identities, supported structures, tag shapes, and portable file references.
It does not fully validate native schemas, Plugin configurations, authentication, endpoints, or remote relationships.

When decK is installed, validate each environment separately:

```sh
deck file validate --analytics=false dist/deck-dev/config.yaml
deck file validate --analytics=false dist/deck-prod/config.yaml
```

Do not pass dev and prod together: decK treats multiple input files as one combined state, which can create duplicate resources.
The included kongctl rules check selected example properties, not full native loading or remote API compatibility:

```sh
kongctl --no-telemetry lint -f dist/kongctl-dev/config.yaml -r examples/kongctl/ruleset.yaml
kongctl --no-telemetry lint -f dist/kongctl-prod/config.yaml -r examples/kongctl/ruleset.yaml
```

For eventual deployment, supply the bundle's `config.yaml` explicitly to your normal native workflow and review the native plan or diff before applying it.
For example, `deck gateway diff dist/deck-prod/config.yaml` and `kongctl plan -f dist/kongctl-prod/config.yaml --output-file plan.json` require a configured target, authentication, and connectivity.
They are not offline validation and are never run by Kongweave.
Do not recursively load the whole bundle: copied assets may otherwise be mistaken for additional configuration files.
Adapt the fictional examples and supply deferred values through the native tool before using them against a real environment.

## Troubleshooting

| Error or symptom | What to check |
| --- | --- |
| `expected 1, actual 0` | Check the kind, name/ref, exact scope, and whether an earlier operation removed the resource; for a field error, verify the field exists |
| `expected 1, actual 2` or more | Add the exact scope or Plugin instance name; explain also considers deleted identities |
| `expected 0, actual 1` | `add` found an existing resource or field; use the intended explicit update operation |
| Duplicate identity | Fix the base or the operation introducing the duplicate; each intermediate result must be valid |
| `identity and scope are immutable` | Preserve all identity/binding fields, or use remove/add and maintain related references yourself |
| `field parent must exist` | Create or merge the containing plain map first; do not traverse arrays or tagged expressions |
| Resource collection error | Select the child kind directly instead of patching an array on its parent |
| Unsupported kind, child structure, or tag | Consult the support tables and isolate unsupported input; unknown structures are not guessed |
| Circular overlay reference | Trace `base.overlay` references, including symlinks; the chain must end at one `base.file` |
| File reference error | Check the declaring file's directory, file existence, and symlink destination; a patch-introduced path belongs beside the patch |
| `output already exists` | Choose a new output directory, including when the existing directory is empty |
| Output lock error | Check for an active writer and output-parent permissions before removing a stale lock |
| `provide overlay directory before flags` | Put the directory immediately after `build` or `explain`; use `./-name` for a directory starting with a hyphen |
| No composition history for a field | Confirm the selected resource and field; native defaults and later tag resolution are outside the history |

Diagnostics deliberately omit payload values and raw YAML parser details.
Use the reported file and line to inspect the source locally rather than expecting secrets or configuration values in an error message.
For the environment configuration problems Kongweave addresses, see [Why Kongweave?](../README.md#why-kongweave).
