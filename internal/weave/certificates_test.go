package weave

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const certificateBase = `ai_gateways:
  - ref: g
    name: g
    data_plane_certificates:
      - ref: g-cert
        title: Cluster Certificate
        cert: !file certs/base.pem
control_planes:
  - ref: cp
    name: cp
control_plane_data_plane_certificates:
  - ref: cp-cert
    control_plane: !ref cp#id
    cert: !file certs/base.pem
`

// Data plane certificates were previously a protected, unsupported child collection.
// Both the nested form and the root form with a parent selector must now be addressable,
// and a certificate introduced by a patch must be copied beside the other assets.
func TestDataPlaneCertificatesAreResources(t *testing.T) {
	patch := `patches:
  - op: replace
    target: {kind: ai_gateway_data_plane_certificates, ref: g-cert, scope: {ai_gateway: g}}
    field: /cert
    value: !file certs/dev.pem
  - op: add
    target: {kind: ai_gateway_data_plane_certificates, ref: g-cert-2, scope: {ai_gateway: g}}
    value: {ref: g-cert-2, title: Second, cert: !file certs/dev.pem}
  - op: merge
    target: {kind: control_plane_data_plane_certificates, ref: cp-cert}
    value: {description: rotated}
`
	entry := fixture(t, "kongctl", certificateBase, patch)
	root := filepath.Dir(filepath.Dir(entry))
	write(t, filepath.Join(root, "base", "certs", "base.pem"), "BASE FIXTURE\n")
	write(t, filepath.Join(entry, "patches", "certs", "dev.pem"), "DEV FIXTURE\n")

	r := mustLoad(t, entry)
	out := filepath.Join(t.TempDir(), "bundle")
	if err := r.Write(out); err != nil {
		t.Fatal(err)
	}
	config, err := os.ReadFile(filepath.Join(out, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(config)
	if strings.Contains(s, "certs/") {
		t.Fatalf("certificate references were not rebased to assets:\n%s", s)
	}
	if strings.Count(s, "!file assets/") != 3 {
		t.Fatalf("expected three relocated certificate references:\n%s", s)
	}
	if !strings.Contains(s, "ref: g-cert-2") || !strings.Contains(s, "description: rotated") {
		t.Fatalf("certificate patches were not applied:\n%s", s)
	}
	nested := selectNode(t, r, Target{Kind: "ai_gateway_data_plane_certificates", Ref: "g-cert-2"})
	if value(nested, "ai_gateway") != "" {
		t.Fatal("a certificate added under its gateway must not carry a scope field")
	}
	entries, err := os.ReadDir(filepath.Join(out, "assets"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected the base and dev fixtures as two assets, got %d", len(entries))
	}
}

func TestDataPlaneCertificateTargetsAreStrict(t *testing.T) {
	for _, c := range []struct{ patch, want string }{
		{"patches:\n  - op: merge\n    target: {kind: ai_gateways, ref: g}\n    value: {data_plane_certificates: []}\n", "resource collections"},
		{"patches:\n  - op: merge\n    target: {kind: ai_gateway_data_plane_certificates, ref: g-cert, scope: {control_plane: cp}}\n    value: {title: x}\n", "unsupported or empty scope"},
		{"patches:\n  - op: add\n    target: {kind: ai_gateway_data_plane_certificates, ref: g-cert-3, scope: {ai_gateway: missing}}\n    value: {ref: g-cert-3}\n", "add parent: expected 1, actual 0"},
	} {
		t.Run(c.want, func(t *testing.T) {
			entry := fixture(t, "kongctl", certificateBase, c.patch)
			write(t, filepath.Join(filepath.Dir(filepath.Dir(entry)), "base", "certs", "base.pem"), "BASE FIXTURE\n")
			_, err := Load(entry)
			assertError(t, err, c.want)
		})
	}
	// The pre-rename spelling is still a recognized, unsupported structure.
	_, err := Load(fixture(t, "kongctl", "control_planes: [{ref: cp, dataplane_certificates: []}]", ""))
	assertError(t, err, "unsupported resource structure")
}
