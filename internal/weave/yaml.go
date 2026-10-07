package weave

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

const maxFileSize = 10 << 20

type source struct {
	File string
	Line int
}

func readLocal(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open local file")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("expected a regular file")
	}
	b, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("cannot read local file")
	}
	if len(b) > maxFileSize {
		return nil, fmt.Errorf("file exceeds 10 MiB limit")
	}
	return b, nil
}

// parse returns the single mapping document in data. record, when non-nil, is
// called for every node so callers can attach provenance without parse itself
// owning any state.
func parse(data []byte, record func(*yaml.Node)) (*yaml.Node, error) {
	d := yaml.NewDecoder(bytes.NewReader(data))
	var doc, extra yaml.Node
	if err := d.Decode(&doc); err != nil {
		// Decoder diagnostics can quote input values, including secrets.
		return nil, fmt.Errorf("invalid YAML (parser details withheld)")
	}
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("expected exactly one YAML document")
	}
	if len(doc.Content) != 1 || !plainMap(doc.Content[0]) {
		return nil, fmt.Errorf("expected a mapping document")
	}
	var visit func(*yaml.Node, int) error
	visit = func(n *yaml.Node, depth int) error {
		if record != nil {
			record(n)
		}
		if depth > 100 {
			return fmt.Errorf("nesting exceeds 100 levels")
		}
		if n.Kind == yaml.AliasNode || n.Anchor != "" {
			return fmt.Errorf("YAML anchors and aliases are unsupported; expand them first")
		}
		if n.Kind == yaml.MappingNode {
			seen := map[string]bool{}
			for i := 0; i < len(n.Content); i += 2 {
				k := n.Content[i]
				if k.Tag != "!!str" || k.Kind != yaml.ScalarNode {
					return fmt.Errorf("mapping keys must be plain strings; YAML merge keys are unsupported")
				}
				if seen[k.Value] {
					return fmt.Errorf("duplicate mapping key at line %d", k.Line)
				}
				seen[k.Value] = true
			}
		}
		for _, c := range n.Content {
			if err := visit(c, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	if err := visit(doc.Content[0], 0); err != nil {
		return nil, err
	}
	return doc.Content[0], nil
}

func (e *engine) read(path string) (*yaml.Node, error) {
	b, err := readLocal(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.display(path), err)
	}
	n, err := parse(b, func(n *yaml.Node) { e.origins[n] = source{path, n.Line} })
	if err != nil {
		return nil, fmt.Errorf("%s: %w", e.display(path), err)
	}
	return n, nil
}

func get(n *yaml.Node, key string) *yaml.Node {
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func put(n *yaml.Node, key string, value *yaml.Node) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content[i+1] = value
			return
		}
	}
	n.Content = append(n.Content, str(key), value)
}

func drop(n *yaml.Node, key string) {
	for i := 0; i < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			n.Content = append(n.Content[:i], n.Content[i+2:]...)
			return
		}
	}
}

func str(s string) *yaml.Node    { return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s} }
func plainMap(n *yaml.Node) bool { return n != nil && n.Kind == yaml.MappingNode && n.Tag == "!!map" }

func textValue(n *yaml.Node) (string, bool) {
	if n != nil && n.Kind == yaml.ScalarNode && n.Tag == "!!str" {
		return n.Value, true
	}
	return "", false
}

func value(n *yaml.Node, key string) string { s, _ := textValue(get(n, key)); return s }

// allowedKeys rejects any mapping key outside allowed. Callers decide whether
// the node must be a plain map or may carry a custom tag.
func allowedKeys(n *yaml.Node, allowed ...string) error {
	for i := 0; i < len(n.Content); i += 2 {
		ok := false
		for _, k := range allowed {
			if n.Content[i].Value == k {
				ok = true
			}
		}
		if !ok {
			return fmt.Errorf("unknown key at line %d", n.Content[i].Line)
		}
	}
	return nil
}

func keys(n *yaml.Node, allowed ...string) error {
	if !plainMap(n) {
		return fmt.Errorf("expected a plain mapping")
	}
	return allowedKeys(n, allowed...)
}

// cloneNode copies n without comments and carries provenance from one origin
// table to another, so a bundle can clone without touching the engine's table.
func cloneNode(n *yaml.Node, from, to map[*yaml.Node]source) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	c.Content = nil
	c.HeadComment = ""
	c.LineComment = ""
	c.FootComment = ""
	to[&c] = from[n]
	for _, child := range n.Content {
		c.Content = append(c.Content, cloneNode(child, from, to))
	}
	return &c
}

func (e *engine) clone(n *yaml.Node) *yaml.Node { return cloneNode(n, e.origins, e.origins) }

func (e *engine) merge(dst, patch *yaml.Node) *yaml.Node {
	// Tagged mappings are expressions (e.g. !secret), not configuration maps.
	if !plainMap(dst) || !plainMap(patch) {
		return e.clone(patch)
	}
	for i := 0; i < len(patch.Content); i += 2 {
		k, p := patch.Content[i].Value, patch.Content[i+1]
		put(dst, k, e.merge(get(dst, k), p))
	}
	return dst
}

func encode(n *yaml.Node) ([]byte, error) {
	var b bytes.Buffer
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(n); err != nil {
		return nil, fmt.Errorf("cannot encode YAML")
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func relativeFile(from, ref string) (string, error) {
	if ref == "" || filepath.IsAbs(ref) || strings.Contains(ref, ":") || strings.Contains(ref, "\\") || ref == "-" {
		return "", fmt.Errorf("expected a relative local path (no URLs, stdin, or absolute paths)")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(from), ref)), nil
}

// pointer follows RFC 6901: an empty reference token such as "/" or "/a/" is
// valid and names the empty-string key.
func pointer(path string) ([]string, error) {
	if path == "" {
		return nil, nil
	}
	if !strings.HasPrefix(path, "/") {
		return nil, fmt.Errorf("field must be a JSON Pointer starting with /; only mapping keys are supported")
	}
	parts := strings.Split(path[1:], "/")
	for i, p := range parts {
		for j := 0; j < len(p); j++ {
			if p[j] == '~' {
				if j+1 >= len(p) || (p[j+1] != '0' && p[j+1] != '1') {
					return nil, fmt.Errorf("invalid JSON Pointer escape")
				}
				j++
			}
		}
		parts[i] = strings.ReplaceAll(strings.ReplaceAll(p, "~1", "/"), "~0", "~")
	}
	return parts, nil
}

func escape(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, "~", "~0"), "/", "~1") }

func fields(n *yaml.Node, prefix string) []string {
	if !plainMap(n) || len(n.Content) == 0 {
		return []string{prefix}
	}
	var out []string
	for i := 0; i < len(n.Content); i += 2 {
		out = append(out, fields(n.Content[i+1], prefix+"/"+escape(n.Content[i].Value))...)
	}
	return out
}
