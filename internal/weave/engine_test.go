package weave

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func fixture(t *testing.T, format, base, patch string) string {
	t.Helper()
	dir := t.TempDir()
	write(t, filepath.Join(dir, "base", "config.yaml"), base)
	overlay := "apiVersion: kongweave/v1alpha1\nformat: " + format + "\nbase:\n  file: ../../base/config.yaml\n"
	if patch != "" {
		overlay += "patches: [patches/change.yaml]\n"
		write(t, filepath.Join(dir, "overlays", "dev", "patches", "change.yaml"), patch)
	}
	entry := filepath.Join(dir, "overlays", "dev")
	write(t, filepath.Join(entry, "overlay.yaml"), overlay)
	return entry
}
func mustLoad(t *testing.T, dir string) *Result {
	t.Helper()
	r, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func selectNode(t *testing.T, r *Result, target Target) *yaml.Node {
	t.Helper()
	rs, err := r.e.adapter.index(r.e.root)
	if err != nil {
		t.Fatal(err)
	}
	found := matches(rs, target)
	if len(found) != 1 {
		t.Fatalf("selected %d resources", len(found))
	}
	return found[0].Node
}
func assertError(t *testing.T, err error, want ...string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected failure")
	}
	for _, s := range want {
		if !strings.Contains(err.Error(), s) {
			t.Fatalf("error %q lacks %q", err, s)
		}
	}
}

const deckBase = `_format_version: "3.0"
services:
  - name: api
    host: base.example.com
    read_timeout: 60000
    routes:
      - name: echo
        paths: [/echo]
        plugins:
          - name: rate-limiting
            config: {minute: 10, policy: local}
    plugins:
      - name: rate-limiting
        config: {minute: 60, policy: local}
consumers:
  - username: alice
    plugins:
      - name: rate-limiting
        config: {minute: 30, policy: local}
plugins:
  - name: rate-limiting
    config: {minute: 5, policy: local}
`

func TestNestedMergeAndPluginScopes(t *testing.T) {
	dir := fixture(t, "deck", deckBase, `patches:
  - op: merge
    target: {kind: plugins, name: rate-limiting, scope: {service: api}}
    value: {config: {minute: 600}}
  - op: merge
    target: {kind: plugins, name: rate-limiting, scope: {route: echo}}
    value: {config: {minute: 100}}
  - op: merge
    target: {kind: plugins, name: rate-limiting, scope: {consumer: alice}}
    value: {config: {minute: 300}}
  - op: merge
    target: {kind: plugins, name: rate-limiting, scope: {}}
    value: {config: {minute: 50}}
`)
	r := mustLoad(t, dir)
	for _, c := range []struct {
		scope  map[string]string
		minute string
	}{{map[string]string{"service": "api"}, "600"}, {map[string]string{"route": "echo"}, "100"}, {map[string]string{"consumer": "alice"}, "300"}, {map[string]string{}, "50"}} {
		n := selectNode(t, r, Target{Kind: "plugins", Name: "rate-limiting", Scope: c.scope})
		if get(get(n, "config"), "minute").Value != c.minute || value(get(n, "config"), "policy") != "local" {
			t.Fatalf("partial merge lost data: %+v", c)
		}
	}
	target := Target{Kind: "plugins", Name: "rate-limiting", Scope: map[string]string{"service": "api"}}
	events, err := r.Explain(target, "/config/minute")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[0].Operation != "base" || events[1].Source != "patches/change.yaml" || events[1].Line != 2 {
		t.Fatalf("unexpected history: %+v", events)
	}
	policy, err := r.Explain(target, "/config/policy")
	if err != nil || len(policy) != 1 {
		t.Fatalf("unmodified field claimed by patch: %+v %v", policy, err)
	}
}

func TestStrictTargetFailures(t *testing.T) {
	for _, c := range []struct {
		name, patch string
		want        []string
	}{
		{"missing", `{op: merge, target: {kind: services, name: typo}, value: {host: typo.example.com}}`, []string{"patches/change.yaml:2", "services", "typo", "expected 1, actual 0"}},
		{"ambiguous", `{op: remove, target: {kind: plugins, name: rate-limiting}}`, []string{"patches/change.yaml:2", "plugins", "expected 1, actual 4"}},
		{"existing add", `{op: add, target: {kind: services, name: api}, value: {name: api}}`, []string{"expected 0, actual 1"}},
		{"unsupported kind", `{op: remove, target: {kind: targets, name: x}}`, []string{"unsupported resource kind"}},
		{"collection merge", `{op: merge, target: {kind: services, name: api}, value: {routes: []}}`, []string{"resource collections"}},
		{"index traversal", `{op: replace, target: {kind: routes, name: echo}, field: /paths/0, value: /other}`, []string{"arrays"}},
		{"rename", `{op: merge, target: {kind: services, name: api}, value: {name: new}}`, []string{"identity and scope"}},
		{"unknown operation", `{op: merje, target: {kind: services, name: api}, value: {}}`, []string{"op must be"}},
		{"unknown patch key", `{op: merge, target: {kind: services, name: api}, values: {host: x}}`, []string{"unknown key"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dir := fixture(t, "deck", deckBase, "patches:\n  - "+c.patch+"\n")
			_, err := Load(dir)
			assertError(t, err, c.want...)
		})
	}
}

func TestDuplicateIdentifiers(t *testing.T) {
	cases := []struct{ format, base string }{
		{"deck", "_format_version: '3.0'\nservices: [{name: api}, {name: api}]\n"},
		{"deck", "_format_version: '3.0'\nplugins: [{name: cors}, {name: cors}]\n"},
		{"kongctl", "apis: [{ref: shared}]\nportals: [{ref: shared}]\n"},
		{"kongctl", "ai_gateways: [{ref: gw, models: [{ref: m}]}]\nai_gateway_models: [{ref: m, ai_gateway: gw}]\n"},
	}
	for _, c := range cases {
		_, err := Load(fixture(t, c.format, c.base, ""))
		assertError(t, err, "duplicate identifier", "expected 1, actual 2")
	}
}

func TestPluginInstanceAndFlatScopes(t *testing.T) {
	base := `_format_version: "3.0"
services: [{name: api, host: example.com}]
routes: [{name: route, service: {name: api}, paths: [/]}]
consumers: [{username: alice}]
plugins:
 - name: cors
   instance_name: first
   service: {name: api}
   consumer: {username: alice}
   config: {origins: [first]}
 - name: cors
   instance_name: second
   service: api
   consumer: alice
   config: {origins: [second]}
`
	patch := `patches:
 - op: merge
   target: {kind: plugins, name: cors, instance_name: second, scope: {service: api, consumer: alice}}
   value: {config: {origins: [changed]}}
`
	r := mustLoad(t, fixture(t, "deck", base, patch))
	n := selectNode(t, r, Target{Kind: "plugins", Name: "cors", InstanceName: "first"})
	if get(get(n, "config"), "origins").Content[0].Value != "first" {
		t.Fatal("wrong plugin changed")
	}
	_, err := Load(fixture(t, "deck", base, strings.ReplaceAll(patch, ", instance_name: second", "")))
	assertError(t, err, "expected 1, actual 2")
}

func TestOperationsAndNullArraySemantics(t *testing.T) {
	base := `_format_version: "3.0"
services: [{name: api, host: base.example.com, read_timeout: 60000}]
plugins:
 - name: custom
   config:
     keep: local
     absent: original
     nullable: previous
     list: [{name: a, value: 1}, {name: b, value: 2}]
     empty: [x]
     remove_me: old
`
	patch := `patches:
 - op: merge
   target: {kind: plugins, name: custom, scope: {}}
   value:
     config:
       nullable: null
       list: [{name: b, value: 9}]
       empty: []
 - op: remove
   target: {kind: plugins, name: custom, scope: {}}
   field: /config/remove_me
 - op: add
   target: {kind: plugins, name: custom, scope: {}}
   field: /config/new_null
   value: null
 - op: replace
   target: {kind: services, name: api}
   value: {name: api, host: replacement.example.com}
 - op: add
   target: {kind: plugins, name: cors, scope: {service: api}}
   value: {name: cors, config: {origins: ['*']}}
 - op: remove
   target: {kind: plugins, name: custom, scope: {}}
   field: /config/new_null
`
	r := mustLoad(t, fixture(t, "deck", base, patch))
	n := get(selectNode(t, r, Target{Kind: "plugins", Name: "custom"}), "config")
	if value(n, "keep") != "local" || value(n, "absent") != "original" || get(n, "nullable").Tag != "!!null" {
		t.Fatal("missing/null semantics lost")
	}
	if len(get(n, "list").Content) != 1 || get(get(n, "list").Content[0], "value").Value != "9" || len(get(n, "empty").Content) != 0 {
		t.Fatal("ordinary arrays were merged or reordered")
	}
	if get(n, "remove_me") != nil || get(n, "new_null") != nil {
		t.Fatal("remove did not delete field")
	}
	s := selectNode(t, r, Target{Kind: "services", Name: "api"})
	if get(s, "read_timeout") != nil {
		t.Fatal("replace retained unspecified field")
	}
	selectNode(t, r, Target{Kind: "plugins", Name: "cors", Scope: map[string]string{"service": "api"}})
	events, err := r.Explain(Target{Kind: "plugins", Name: "custom"}, "/config/new_null")
	if err != nil || len(events) != 2 || events[1].Operation != "remove" {
		t.Fatalf("deleted field history: %+v %v", events, err)
	}
}

func TestRemoveAndReplaceParentHistory(t *testing.T) {
	for _, op := range []string{"remove", "replace"} {
		t.Run(op, func(t *testing.T) {
			patch := "patches:\n - op: " + op + "\n   target: {kind: services, name: api}\n"
			if op == "replace" {
				patch += "   value: {name: api, host: new.example.com}\n"
			}
			r := mustLoad(t, fixture(t, "deck", deckBase, patch))
			events, err := r.Explain(Target{Kind: "routes", Name: "echo"}, "/paths")
			if err != nil {
				t.Fatal(err)
			}
			if len(events) != 2 || events[1].Operation != "remove" {
				t.Fatalf("lost cascade: %+v", events)
			}
		})
	}
}

func TestFieldOperationPreconditions(t *testing.T) {
	for _, c := range []struct{ op, field, val, want string }{
		{"merge", "/missing", "{}", "expected 1, actual 0"}, {"replace", "/missing", "null", "expected 1, actual 0"},
		{"remove", "/missing", "", "expected 1, actual 0"}, {"add", "/host", "x", "expected 0, actual 1"},
		{"merge", "/host", "{}", "plain mappings"}, {"replace", "/name", "renamed", "immutable"},
		{"replace", "/routes", "[]", "resource collections"},
	} {
		t.Run(c.op+c.field, func(t *testing.T) {
			p := "patches:\n - op: " + c.op + "\n   target: {kind: services, name: api}\n   field: " + c.field + "\n"
			if c.val != "" {
				p += "   value: " + c.val + "\n"
			}
			_, err := Load(fixture(t, "deck", deckBase, p))
			assertError(t, err, c.want)
		})
	}
}

func TestInheritanceOrderAndCycles(t *testing.T) {
	dir := fixture(t, "deck", deckBase, `patches:
 - op: merge
   target: {kind: services, name: api}
   value: {host: dev.example.com}
`)
	prod := filepath.Join(filepath.Dir(dir), "prod")
	write(t, filepath.Join(prod, "overlay.yaml"), "apiVersion: kongweave/v1alpha1\nformat: deck\nbase: {overlay: ../dev/overlay.yaml}\npatches: [a.yaml, b.yaml]\n")
	write(t, filepath.Join(prod, "a.yaml"), "patches: [{op: merge, target: {kind: services, name: api}, value: {host: first.example.com}}]\n")
	write(t, filepath.Join(prod, "b.yaml"), "patches: [{op: merge, target: {kind: services, name: api}, value: {host: last.example.com}}]\n")
	r := mustLoad(t, prod)
	if value(selectNode(t, r, Target{Kind: "services", Name: "api"}), "host") != "last.example.com" {
		t.Fatal("wrong application order")
	}
	events, err := r.Explain(Target{Kind: "services", Name: "api"}, "/host")
	if err != nil || len(events) != 4 || events[2].Source != "a.yaml" || events[3].Source != "b.yaml" {
		t.Fatalf("wrong event order: %+v %v", events, err)
	}
	write(t, filepath.Join(dir, "overlay.yaml"), "apiVersion: kongweave/v1alpha1\nformat: deck\nbase: {overlay: ../prod/overlay.yaml}\n")
	_, err = Load(prod)
	assertError(t, err, "circular overlay reference")
}

func TestUnsupportedAndMalformedInput(t *testing.T) {
	for _, c := range []struct{ format, base, want string }{
		{"deck", "_format_version: '3.0'\nservices: [{name: x, host: a, host: b}]", "duplicate mapping key"},
		{"deck", "_format_version: '3.0'\nservices: &x []", "anchors and aliases"},
		{"deck", "_format_version: '3.0'\n---\nservices: []", "exactly one YAML document"},
		{"deck", "_format_version: '3.0'\nportals: []", "unsupported top-level"},
		{"kongctl", "_format_version: '3.0'\nservices: []", "unsupported top-level"},
		{"kongctl", "_templates: {x: {name: y}}\nportals: []", "unsupported"},
		{"kongctl", "portals: [{ref: p, _extends: x}]", "_extends"},
		{"kongctl", "portals: [{ref: !env NAME}]", "explicit non-empty string ref"},
		{"kongctl", "ai_gateways: [{ref: g, vaults: []}]", "unsupported resource structure"},
		{"deck", "_format_version: '3.0'\nservices: [{host: unnamed.example.com}]", "explicit non-empty string name"},
		{"deck", "_format_version: '3.0'\nplugins: [{name: cors, service: {id: a-uuid}}]", "unsupported parent/scope"},
		{"deck", "_format_version: '3.0'\nplugins: null", "must be a resource array"},
		{"kongctl", "apis: [{ref: x, description: !lookup {name: !file x}}]", "lookup values"},
	} {
		t.Run(c.want, func(t *testing.T) { _, err := Load(fixture(t, c.format, c.base, "")); assertError(t, err, c.want) })
	}
}

func TestTagsAndPortableBundle(t *testing.T) {
	base := `_defaults:
  kongctl: {namespace: demo}
control_planes:
 - ref: cp
   name: demo
   _deck:
     files: [gateway.yaml]
apis:
 - ref: api
   name: Demo
   description: !file description.md
   versions:
    - ref: version
      version: v1
      spec: !file {path: spec.yaml, extract: info}
   publications:
    - ref: pub
      portal_id: !ref portal#id
portals:
 - ref: portal
   name: Portal
   description: !env {var: UNSET_KONGWEAVE_TEST_VAR, extract: text}
ai_gateways:
 - ref: ai
   name: ai
   model_providers:
    - ref: provider
      name: provider
      type: openai
      config:
        auth:
          type: basic
          headers:
           - name: Authorization
             value: !secret {parts: ['Bearer ', !env NEVER_RESOLVE]}
`
	patch := `patches:
 - op: merge
   target: {kind: apis, ref: api}
   value: {description: !file patch.md#title}
 - op: add
   target: {kind: api_publications, ref: extra, scope: {api: api}}
   value: {ref: extra, portal_id: !lookup {name: !env PORTAL}}
 - op: add
   target: {kind: api_publications, ref: legacy, scope: {api: api}}
   value: {ref: legacy, portal_id: !external 'name:Legacy'}
`
	dir := fixture(t, "kongctl", base, patch)
	baseDir := filepath.Join(dir, "..", "..", "base")
	write(t, filepath.Join(baseDir, "gateway.yaml"), "_format_version: '3.0'\nservices: []\n")
	write(t, filepath.Join(baseDir, "spec.yaml"), "info: {title: example}\n")
	write(t, filepath.Join(dir, "patches", "patch.md"), "title: Patched document\n")
	// The removed reference is not loaded during composition or packaging.
	r := mustLoad(t, dir)
	out := filepath.Join(t.TempDir(), "relocated", "bundle")
	if err := r.Write(out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(out, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range []string{"!file", "!env", "!secret", "!ref", "!lookup", "!external", "_defaults:"} {
		if !bytes.Contains(data, []byte(tag)) {
			t.Errorf("lost %s", tag)
		}
	}
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		t.Fatal(err)
	}
	api := get(n.Content[0], "apis").Content[0]
	ref := get(api, "description").Value
	asset := strings.SplitN(ref, "#", 2)[0]
	assetData, err := os.ReadFile(filepath.Join(out, asset))
	if err != nil || string(assetData) != "title: Patched document\n" {
		t.Fatal("patch-relative reference changed")
	}
	fileMap := get(get(api, "versions").Content[0], "spec")
	if value(fileMap, "extract") != "info" {
		t.Fatal("lost file extraction")
	}
	if _, err := os.Stat(filepath.Join(out, value(fileMap, "path"))); err != nil {
		t.Fatal(err)
	}
	deckPath := get(get(get(n.Content[0], "control_planes").Content[0], "_deck"), "files").Content[0].Value
	if _, err := os.Stat(filepath.Join(out, deckPath)); err != nil {
		t.Fatal(err)
	}
	out2 := filepath.Join(t.TempDir(), "second")
	if err := r.Write(out2); err != nil {
		t.Fatal(err)
	}
	assertBundlesEqual(t, out, out2)
}

func assertBundlesEqual(t *testing.T, a, b string) {
	t.Helper()
	err := filepath.WalkDir(a, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(a, path)
		x, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		y, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil {
			return err
		}
		if !bytes.Equal(x, y) {
			t.Errorf("non-deterministic artifact %s", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeferredSecretFileIsOpaqueAndPrivate(t *testing.T) {
	dir := fixture(t, "kongctl", "ai_gateways: [{ref: g, model_providers: [{ref: p, config: {auth: {headers: [{name: x, value: !secret {source: !file dummy.key}}]}}}]}]\n", "")
	write(t, filepath.Join(dir, "..", "..", "base", "dummy.key"), "PUBLIC-TEST-FIXTURE\n")
	r := mustLoad(t, dir)
	out := filepath.Join(t.TempDir(), "bundle")
	if err := r.Write(out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "config.yaml"))
	metadata, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
	if bytes.Contains(data, []byte("PUBLIC-TEST-FIXTURE")) || bytes.Contains(metadata, []byte("PUBLIC-TEST-FIXTURE")) {
		t.Fatal("deferred file was evaluated")
	}
	err := filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err == nil && info.Mode().Perm()&0077 != 0 {
			t.Errorf("non-private permissions: %s", p)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPublicationFailurePreservesExistingAndLeavesNoPartialBundle(t *testing.T) {
	dir := fixture(t, "kongctl", "apis: [{ref: api, description: !file missing.md}]\n", "")
	r := mustLoad(t, dir)
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	if err := r.Write(out); err == nil {
		t.Fatal("missing asset accepted")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("partial output published")
	}
	write(t, filepath.Join(out, "config.yaml"), "KNOWN-GOOD\n")
	err := r.Write(out)
	assertError(t, err, "output already exists")
	data, _ := os.ReadFile(filepath.Join(out, "config.yaml"))
	if string(data) != "KNOWN-GOOD\n" {
		t.Fatal("existing artifact changed")
	}
	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 {
		t.Fatal("staging garbage remained")
	}
}

func TestAssetBoundaryAndRedaction(t *testing.T) {
	for _, ref := range []string{"../outside.md", "https://example.com/a", "/etc/passwd", "*.yaml", "-"} {
		dir := fixture(t, "kongctl", "apis: [{ref: api, description: !file '"+ref+"'}]\n", "")
		r := mustLoad(t, dir)
		err := r.Write(filepath.Join(t.TempDir(), "out"))
		assertError(t, err, "file reference", "value withheld")
	}
	dir := fixture(t, "kongctl", "apis: [{ref: api, description: !file linked.md}]\n", "")
	outside := filepath.Join(t.TempDir(), "secret.md")
	write(t, outside, "DO-NOT-PRINT")
	if err := os.Symlink(outside, filepath.Join(dir, "..", "..", "base", "linked.md")); err != nil {
		t.Skip(err)
	}
	err := mustLoad(t, dir).Write(filepath.Join(t.TempDir(), "out"))
	assertError(t, err, "including symlinks")
	if strings.Contains(err.Error(), "DO-NOT-PRINT") {
		t.Fatal("error leaked value")
	}
}

func TestExplainNeverContainsValues(t *testing.T) {
	dir := fixture(t, "deck", deckBase, "patches: [{op: merge, target: {kind: services, name: api}, value: {host: DO-NOT-PRINT}}]\n")
	r := mustLoad(t, dir)
	events, err := r.Explain(Target{Kind: "services", Name: "api"}, "/host")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(events)
	if bytes.Contains(data, []byte("DO-NOT-PRINT")) {
		t.Fatal("history leaked value")
	}
}
