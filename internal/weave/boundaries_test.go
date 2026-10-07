package weave

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFlatKongctlChildrenAndExactScope(t *testing.T) {
	base := `ai_gateways:
 - ref: first
   models: [{ref: nested, name: nested}]
 - ref: second
ai_gateway_models:
 - ref: flat
   ai_gateway: !ref second
   name: flat
   config: {route: {paths: [/a, /b], strip_path: true}}
`
	patch := `patches:
 - op: merge
   target: {kind: ai_gateway_models, ref: flat, scope: {ai_gateway: second}}
   value: {config: {route: {paths: [/b, /a]}}}
`
	r := mustLoad(t, fixture(t, "kongctl", base, patch))
	route := get(get(selectNode(t, r, Target{Kind: "ai_gateway_models", Ref: "flat"}), "config"), "route")
	if get(route, "strip_path").Value != "true" || get(route, "paths").Content[0].Value != "/b" || get(route, "paths").Content[1].Value != "/a" {
		t.Fatal("route fields or array order changed")
	}
	_, err := Load(fixture(t, "kongctl", base, strings.ReplaceAll(patch, "ai_gateway: second", "ai_gateway: first")))
	assertError(t, err, "expected 1, actual 0")
}

func TestAddCombinedScopeAndRejectDuplicateInstance(t *testing.T) {
	patch := `patches:
 - op: add
   target: {kind: plugins, name: cors, scope: {service: api, consumer: alice}}
   value: {name: cors, config: {origins: ['*']}}
`
	r := mustLoad(t, fixture(t, "deck", deckBase, patch))
	n := selectNode(t, r, Target{Kind: "plugins", Name: "cors", Scope: map[string]string{"service": "api", "consumer": "alice"}})
	if value(n, "service") != "api" || value(n, "consumer") != "alice" {
		t.Fatal("combined scope not represented in native YAML")
	}
	duplicate := deckBase + "  - name: cors\n    instance_name: same\n  - name: cors\n    instance_name: same\n    service: api\n"
	_, err := Load(fixture(t, "deck", duplicate, ""))
	assertError(t, err, "duplicate instance_name")
}

func TestInheritanceRejectsMixedFormatsAndSymlinkCycles(t *testing.T) {
	dir := fixture(t, "deck", deckBase, "")
	child := filepath.Join(t.TempDir(), "child")
	// Overlay references deliberately may cross directories; asset references may not.
	rel, _ := filepath.Rel(child, filepath.Join(dir, "overlay.yaml"))
	write(t, filepath.Join(child, "overlay.yaml"), "apiVersion: kongweave/v1alpha1\nformat: kongctl\nbase: {overlay: "+filepath.ToSlash(rel)+"}\n")
	_, err := Load(child)
	assertError(t, err, "mixed formats")
	write(t, filepath.Join(dir, "overlay.yaml"), "apiVersion: kongweave/v1alpha1\nformat: deck\nbase: {overlay: alias.yaml}\n")
	if err := os.Symlink("overlay.yaml", filepath.Join(dir, "alias.yaml")); err != nil {
		t.Skip(err)
	}
	_, err = Load(dir)
	assertError(t, err, "circular overlay reference")
}

func TestSourceLocationDoesNotAffectBundleBytes(t *testing.T) {
	a := fixture(t, "deck", deckBase, "")
	b := fixture(t, "deck", deckBase, "")
	out1 := filepath.Join(t.TempDir(), "a")
	out2 := filepath.Join(t.TempDir(), "b")
	if err := mustLoad(t, a).Write(out1); err != nil {
		t.Fatal(err)
	}
	if err := mustLoad(t, b).Write(out2); err != nil {
		t.Fatal(err)
	}
	assertBundlesEqual(t, out1, out2)
}

func TestTaggedExpressionsAreAtomic(t *testing.T) {
	base := "apis: [{ref: api, description: !env {var: FIRST, extract: a}}]\n"
	patch := "patches: [{op: merge, target: {kind: apis, ref: api}, value: {description: !env SECOND}}]\n"
	r := mustLoad(t, fixture(t, "kongctl", base, patch))
	n := get(selectNode(t, r, Target{Kind: "apis", Ref: "api"}), "description")
	if n.Tag != "!env" || n.Value != "SECOND" || len(n.Content) != 0 {
		t.Fatal("tag expression was recursively merged")
	}
	patch = "patches: [{op: replace, target: {kind: apis, ref: api}, field: /description/var, value: SECOND}]\n"
	_, err := Load(fixture(t, "kongctl", base, patch))
	assertError(t, err, "tagged expressions cannot be traversed")
}

func TestPatchFileReferencesAndDeckReplacement(t *testing.T) {
	base := "control_planes: [{ref: cp, name: cp, _deck: {files: [missing.yaml]}}]\n"
	patch := "patches: [{op: merge, target: {kind: control_planes, ref: cp}, value: {_deck: {files: [new.yaml]}}}]\n"
	dir := fixture(t, "kongctl", base, patch)
	write(t, filepath.Join(dir, "patches", "new.yaml"), "_format_version: '3.0'\nservices: []\n")
	out := filepath.Join(t.TempDir(), "out")
	if err := mustLoad(t, dir).Write(out); err != nil {
		t.Fatal(err)
	}
}

func TestMalformedValuesAreNotPrinted(t *testing.T) {
	_, err := Load(fixture(t, "kongctl", "apis: [{ref: api, description: [DO-NOT-PRINT}\n", ""))
	assertError(t, err, "parser details withheld")
	if strings.Contains(err.Error(), "DO-NOT-PRINT") {
		t.Fatal("parser error leaked value")
	}
	_, err = Load(fixture(t, "kongctl", "apis: [{ref: api, description: !<tag:example.com,2026:unknown> private}]\n", ""))
	assertError(t, err, "unsupported custom YAML tag")
}

func TestResourceAddNeverRenamesOrRescopes(t *testing.T) {
	for _, patch := range []string{
		"{op: add, target: {kind: services, name: wanted}, value: {name: different}}",
		"{op: add, target: {kind: plugins, name: cors, scope: {service: api}}, value: {name: cors, service: different}}",
		"{op: add, target: {kind: plugins, name: cors}, value: {name: cors}}",
	} {
		_, err := Load(fixture(t, "deck", deckBase, "patches: ["+patch+"]\n"))
		if err == nil {
			t.Fatal("accepted mismatched or implicit identity")
		}
	}
}

func TestOutputLockAndUnsafeDeckFlags(t *testing.T) {
	dir := fixture(t, "deck", deckBase, "")
	r := mustLoad(t, dir)
	parent := t.TempDir()
	out := filepath.Join(parent, "bundle")
	write(t, out+".kongweave-lock", "busy")
	assertError(t, r.Write(out), "locked")
	entries, _ := os.ReadDir(parent)
	if len(entries) != 1 {
		t.Fatal("failed publication left staging output")
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Fatal("locked output published")
	}
	dir = fixture(t, "kongctl", "control_planes: [{ref: cp, _deck: {files: [a.yaml], flags: ['--ca-cert-file=ca.pem']}}]\n", "")
	assertError(t, mustLoad(t, dir).Write(filepath.Join(t.TempDir(), "out")), "_deck.flags are unsupported")
}

func TestNullAndScalarTypesSurviveEncoding(t *testing.T) {
	base := "_format_version: '3.0'\nplugins: [{name: custom, config: {leading: '001', yes_word: 'yes', enabled: true, integer: 3, decimal: 1.5, nullable: null}}]\n"
	r := mustLoad(t, fixture(t, "deck", base, ""))
	out := filepath.Join(t.TempDir(), "out")
	if err := r.Write(out); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(out, "config.yaml"))
	parsed := r.e
	path := filepath.Join(t.TempDir(), "encoded.yaml")
	write(t, path, string(data))
	n, err := parsed.read(path)
	if err != nil {
		t.Fatal(err)
	}
	config := get(get(n, "plugins").Content[0], "config")
	for key, tag := range map[string]string{"leading": "!!str", "yes_word": "!!str", "enabled": "!!bool", "integer": "!!int", "decimal": "!!float", "nullable": "!!null"} {
		if get(config, key).Tag != tag {
			t.Errorf("lost type for %s", key)
		}
	}
}

func TestCombinedScopeAddIsDeterministic(t *testing.T) {
	patch := `patches:
 - op: add
   target: {kind: plugins, name: cors, scope: {service: api, consumer: alice}}
   value: {name: cors, config: {origins: ['*']}}
`
	var first []byte
	for i := 0; i < 32; i++ {
		r := mustLoad(t, fixture(t, "deck", deckBase, patch))
		data, err := encode(r.e.root)
		if err != nil {
			t.Fatal(err)
		}
		if first == nil {
			first = data
		} else if !bytes.Equal(first, data) {
			t.Fatal("identical input produced different YAML")
		}
	}
	n := selectNode(t, mustLoad(t, fixture(t, "deck", deckBase, patch)), Target{Kind: "plugins", Name: "cors", Scope: map[string]string{"service": "api", "consumer": "alice"}})
	if n.Content[len(n.Content)-4].Value != "consumer" || n.Content[len(n.Content)-2].Value != "service" {
		t.Fatal("synthesized scope fields are not in fixed order")
	}
}

func TestEmptyPointerTokensFollowRFC6901(t *testing.T) {
	// "/" names the empty-string key. It is absent, so the failure is cardinality,
	// not a misreport about identity fields.
	_, err := Load(fixture(t, "deck", deckBase, "patches: [{op: replace, target: {kind: services, name: api}, field: /, value: x}]\n"))
	assertError(t, err, "field target: expected 1, actual 0")
	if strings.Contains(err.Error(), "immutable") {
		t.Fatalf("misdiagnosed empty key: %v", err)
	}
	r := mustLoad(t, fixture(t, "deck", deckBase, "patches: [{op: add, target: {kind: services, name: api}, field: /, value: x}]\n"))
	if value(selectNode(t, r, Target{Kind: "services", Name: "api"}), "") != "x" {
		t.Fatal("empty-string key was not written")
	}
	_, err = Load(fixture(t, "deck", deckBase, "patches: [{op: replace, target: {kind: services, name: api}, field: /host/, value: x}]\n"))
	assertError(t, err, "field parent must exist and be a plain mapping")
}

func TestDiagnosticsOmitSatisfiedCardinality(t *testing.T) {
	_, err := Load(fixture(t, "deck", deckBase, "patches: [{op: merge, target: {kind: services, name: api}, value: {name: new}}]\n"))
	assertError(t, err, "immutable")
	if strings.Contains(err.Error(), "actual 1") {
		t.Fatalf("satisfied cardinality reported as a problem: %v", err)
	}
	// An omitted scope and an explicit empty scope are different selectors.
	if s := (Target{Kind: "plugins", Name: "cors"}).String(); strings.Contains(s, "scope") {
		t.Fatalf("omitted scope printed: %s", s)
	}
	if s := (Target{Kind: "plugins", Name: "cors", Scope: map[string]string{}}).String(); !strings.Contains(s, `"scope":{}`) {
		t.Fatalf("global scope lost: %s", s)
	}
}

func TestScopeContractForAddReplaceAndNesting(t *testing.T) {
	base := `_format_version: "3.0"
services: [{name: api, host: a}]
consumers: [{username: alice}]
plugins: [{name: cors, service: api, config: {origins: ['*']}}]
`
	// add: a single scope nests the plugin under its parent without a scope field.
	r := mustLoad(t, fixture(t, "deck", base, "patches: [{op: add, target: {kind: plugins, name: cors, scope: {consumer: alice}}, value: {name: cors}}]\n"))
	n := selectNode(t, r, Target{Kind: "plugins", Name: "cors", Scope: map[string]string{"consumer": "alice"}})
	if get(n, "consumer") != nil || len(get(get(r.e.root, "consumers").Content[0], "plugins").Content) != 1 {
		t.Fatal("single-scope add was not nested")
	}
	// replace: the value is the whole resource, so a flat plugin's scope must be restated.
	_, err := Load(fixture(t, "deck", base, "patches: [{op: replace, target: {kind: plugins, name: cors, scope: {service: api}}, value: {name: cors}}]\n"))
	assertError(t, err, "immutable")
	r = mustLoad(t, fixture(t, "deck", base, "patches: [{op: replace, target: {kind: plugins, name: cors, scope: {service: api}}, value: {name: cors, service: api}}]\n"))
	if get(selectNode(t, r, Target{Kind: "plugins", Name: "cors", Scope: map[string]string{"service": "api"}}), "config") != nil {
		t.Fatal("replace retained an unspecified field")
	}
	// nested: scope comes from the parent, so a nested replace needs no scope field.
	r = mustLoad(t, fixture(t, "deck", deckBase, "patches: [{op: replace, target: {kind: routes, name: echo}, value: {name: echo, paths: [/new]}}]\n"))
	selectNode(t, r, Target{Kind: "routes", Name: "echo", Scope: map[string]string{"service": "api"}})
}

func TestWriteDoesNotMutateEngine(t *testing.T) {
	dir := fixture(t, "kongctl", "control_planes: [{ref: cp, name: cp, _deck: {files: [gw.yaml]}}]\napis: [{ref: api, description: !file doc.md}]\n", "")
	base := filepath.Join(dir, "..", "..", "base")
	write(t, filepath.Join(base, "gw.yaml"), "_format_version: '3.0'\nservices: []\n")
	write(t, filepath.Join(base, "doc.md"), "doc\n")
	r := mustLoad(t, dir)
	origins := len(r.e.origins)
	before, _ := encode(r.e.root)
	for _, out := range []string{"a", "b"} {
		if err := r.Write(filepath.Join(t.TempDir(), out)); err != nil {
			t.Fatal(err)
		}
	}
	after, _ := encode(r.e.root)
	if len(r.e.origins) != origins || !bytes.Equal(before, after) {
		t.Fatal("Write changed engine state")
	}
}

func TestWholeResourceOperationsCannotIntroduceInstanceName(t *testing.T) {
	for _, patch := range []string{
		"patches: [{op: merge, target: {kind: plugins, name: rate-limiting, scope: {service: api}}, value: {instance_name: named}}]\n",
		"patches: [{op: replace, target: {kind: plugins, name: rate-limiting, scope: {service: api}}, value: {name: rate-limiting, instance_name: named}}]\n",
	} {
		// The selector omits instance_name as a wildcard; the identity check must not.
		_, err := Load(fixture(t, "deck", deckBase, patch))
		assertError(t, err, "immutable")
	}
}
