package weave

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type Event struct {
	Order     int      `json:"order"`
	Source    string   `json:"source"`
	Line      int      `json:"line"`
	Operation string   `json:"operation"`
	Target    Target   `json:"target"`
	Fields    []string `json:"fields"`
}

type Result struct {
	Format  string
	History []Event
	e       *engine
}

type engine struct {
	entry   string
	root    *yaml.Node
	adapter adapter
	origins map[*yaml.Node]source
	// resources is the index of root as of the latest successful validate().
	// Every mutation path revalidates, so it is always current when apply runs.
	resources []*resource
	stack     map[string]bool
	history   []Event
}

// operation is one parsed patch entry.
type operation struct {
	Op     string
	Target Target
	Field  string
	parts  []string
	Value  *yaml.Node
	origin source
}

func Load(directory string) (*Result, error) {
	entry, err := filepath.Abs(filepath.Join(directory, "overlay.yaml"))
	if err != nil {
		return nil, err
	}
	e := &engine{entry: entry, origins: map[*yaml.Node]source{}, stack: map[string]bool{}}
	if err := e.loadOverlay(entry, 0); err != nil {
		return nil, err
	}
	return &Result{Format: e.adapter.Format, History: e.history, e: e}, nil
}

func (e *engine) display(path string) string {
	s, err := filepath.Rel(filepath.Dir(e.entry), path)
	if err != nil {
		return filepath.Base(path)
	}
	return filepath.ToSlash(s)
}

func (e *engine) loadOverlay(path string, depth int) error {
	if depth > 64 {
		return fmt.Errorf("overlay inheritance exceeds 64 levels")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("%s: cannot open overlay", e.display(path))
	}
	if e.stack[real] {
		return fmt.Errorf("%s: circular overlay reference", e.display(path))
	}
	e.stack[real] = true
	defer delete(e.stack, real)
	n, err := e.read(path)
	if err != nil {
		return err
	}
	if err := keys(n, "apiVersion", "format", "base", "patches"); err != nil {
		return fmt.Errorf("%s: %w", e.display(path), err)
	}
	if value(n, "apiVersion") != "kongweave/v1alpha1" {
		return fmt.Errorf("%s: apiVersion must be kongweave/v1alpha1", e.display(path))
	}
	a, err := newAdapter(value(n, "format"))
	if err != nil {
		return fmt.Errorf("%s: %w", e.display(path), err)
	}
	if e.adapter.Format != "" && e.adapter.Format != a.Format {
		return fmt.Errorf("%s: mixed formats are unsupported", e.display(path))
	}
	e.adapter = a
	base := get(n, "base")
	if err := keys(base, "file", "overlay"); err != nil {
		return fmt.Errorf("%s: base: %w", e.display(path), err)
	}
	if len(base.Content) != 2 {
		return fmt.Errorf("%s: base must contain exactly one of file or overlay", e.display(path))
	}
	ref, ok := textValue(base.Content[1])
	if !ok {
		return fmt.Errorf("%s: base path must be a string", e.display(path))
	}
	next, err := relativeFile(path, ref)
	if err != nil {
		return fmt.Errorf("%s: base: %w", e.display(path), err)
	}
	if base.Content[0].Value == "overlay" {
		if err := e.loadOverlay(next, depth+1); err != nil {
			return err
		}
	} else {
		e.root, err = e.read(next)
		if err != nil {
			return err
		}
		if err := e.validate(); err != nil {
			return fmt.Errorf("%s: %w", e.display(next), err)
		}
		for _, r := range e.resources {
			e.record("base", r.Target, fields(r.Node, ""), e.origins[r.Node])
		}
	}
	patches := get(n, "patches")
	if patches == nil {
		return nil
	}
	if patches.Kind != yaml.SequenceNode || patches.Tag != "!!seq" {
		return fmt.Errorf("%s: patches must be an ordered list of relative file paths", e.display(path))
	}
	for _, p := range patches.Content {
		ref, ok := textValue(p)
		if !ok {
			return fmt.Errorf("%s: patch path must be a string", e.display(path))
		}
		file, err := relativeFile(path, ref)
		if err != nil {
			return fmt.Errorf("%s: patches: %w", e.display(path), err)
		}
		if err := e.applyFile(file); err != nil {
			return err
		}
	}
	return nil
}

func parseTarget(n *yaml.Node) (Target, error) {
	t := Target{}
	if err := keys(n, "kind", "name", "ref", "instance_name", "scope"); err != nil {
		return t, err
	}
	for key, dst := range map[string]*string{"kind": &t.Kind, "name": &t.Name, "ref": &t.Ref, "instance_name": &t.InstanceName} {
		if v := get(n, key); v != nil {
			s, ok := textValue(v)
			if !ok || s == "" {
				return t, fmt.Errorf("target identifiers must be non-empty strings")
			}
			*dst = s
		}
	}
	if scope := get(n, "scope"); scope != nil {
		if !plainMap(scope) {
			return t, fmt.Errorf("scope must be a plain mapping")
		}
		t.Scope = map[string]string{}
		for i := 0; i < len(scope.Content); i += 2 {
			s, ok := textValue(scope.Content[i+1])
			if !ok || s == "" {
				return t, fmt.Errorf("scope identifiers must be non-empty strings")
			}
			t.Scope[scope.Content[i].Value] = s
		}
	}
	return t, nil
}

func (e *engine) validate() error {
	if err := validateTags(e.root, e.adapter.Format); err != nil {
		return err
	}
	rs, err := e.adapter.index(e.root)
	if err != nil {
		return err
	}
	e.resources = rs
	return nil
}

func (e *engine) applyFile(file string) error {
	n, err := e.read(file)
	if err != nil {
		return err
	}
	if err := keys(n, "patches"); err != nil {
		return fmt.Errorf("%s: %w", e.display(file), err)
	}
	list := get(n, "patches")
	if list == nil || list.Kind != yaml.SequenceNode || list.Tag != "!!seq" || len(list.Content) == 0 {
		return fmt.Errorf("%s: patches must be a non-empty ordered array", e.display(file))
	}
	for _, p := range list.Content {
		if err := e.apply(p); err != nil {
			return fmt.Errorf("%s:%d: %w", e.display(file), p.Line, err)
		}
	}
	return nil
}

func (e *engine) parseOperation(p *yaml.Node) (operation, error) {
	o := operation{origin: e.origins[p]}
	if err := keys(p, "op", "target", "field", "value"); err != nil {
		return o, err
	}
	t, err := parseTarget(get(p, "target"))
	if err != nil {
		return o, fmt.Errorf("invalid target: %w", err)
	}
	if err := e.adapter.validateTarget(t); err != nil {
		return o, err
	}
	o.Target = t
	o.Op = value(p, "op")
	if o.Op != "merge" && o.Op != "replace" && o.Op != "add" && o.Op != "remove" {
		return o, fmt.Errorf("target %s: op must be merge, replace, add, or remove", t.String())
	}
	if n := get(p, "field"); n != nil {
		s, ok := textValue(n)
		if !ok {
			return o, fmt.Errorf("field must be a string")
		}
		o.Field = s
	}
	if o.parts, err = pointer(o.Field); err != nil {
		return o, err
	}
	o.Value = get(p, "value")
	if o.Op == "remove" && o.Value != nil {
		return o, fmt.Errorf("target %s: remove forbids value", t.String())
	}
	if o.Op != "remove" && o.Value == nil {
		return o, fmt.Errorf("target %s: value is required", t.String())
	}
	return o, nil
}

func (e *engine) apply(p *yaml.Node) error {
	o, err := e.parseOperation(p)
	if err != nil {
		return err
	}
	label := fmt.Sprintf("target %s field %q", o.Target.String(), o.Field)
	found := matches(e.resources, o.Target)
	expected := 1
	if o.Op == "add" && len(o.parts) == 0 {
		expected = 0
	}
	if len(found) != expected {
		return fmt.Errorf("%s: expected %d, actual %d", label, expected, len(found))
	}
	if len(o.parts) > 0 {
		err = e.applyField(o, found[0])
	} else {
		err = e.applyResource(o, found)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", label, err)
	}
	return nil
}

func (e *engine) applyField(o operation, r *resource) error {
	if e.adapter.protectedCollection(o.Target.Kind, o.parts[0]) {
		return fmt.Errorf("resource collections require a resource target; array traversal is unsupported")
	}
	if e.adapter.immutableField(o.Target.Kind, o.parts[0]) {
		return fmt.Errorf("identity and scope fields are immutable; remove and add the resource")
	}
	container := r.Node
	for _, part := range o.parts[:len(o.parts)-1] {
		container = get(container, part)
		if !plainMap(container) {
			return fmt.Errorf("field parent must exist and be a plain mapping; arrays and tagged expressions cannot be traversed")
		}
	}
	key := o.parts[len(o.parts)-1]
	old := get(container, key)
	if o.Op == "add" && old != nil {
		return fmt.Errorf("field target: expected 0, actual 1")
	}
	if o.Op != "add" && old == nil {
		return fmt.Errorf("field target: expected 1, actual 0")
	}
	switch o.Op {
	case "merge":
		if !plainMap(old) || !plainMap(o.Value) {
			return fmt.Errorf("merge requires existing and patch plain mappings")
		}
		put(container, key, e.merge(old, o.Value))
	case "remove":
		drop(container, key)
	default:
		put(container, key, e.clone(o.Value))
	}
	if err := e.validate(); err != nil {
		return err
	}
	paths := []string{o.Field}
	if o.Op == "merge" {
		paths = fields(o.Value, o.Field)
	}
	e.record(o.Op, r.Target, paths, o.origin)
	return nil
}

func (e *engine) applyResource(o operation, found []*resource) error {
	if o.Op != "remove" && !plainMap(o.Value) {
		return fmt.Errorf("resource value must be a plain mapping")
	}
	previous := e.resources
	before := map[string][]byte{}
	for _, r := range previous {
		before[e.adapter.identityKey(r.Target)], _ = encode(r.Node)
	}
	var actual Target
	switch o.Op {
	case "add":
		n := e.clone(o.Value)
		list, parent, err := e.adapter.addList(e.root, previous, o.Target, n)
		if err != nil {
			return err
		}
		d, _ := e.adapter.descriptor(o.Target.Kind)
		if actual, err = e.adapter.identify(d, n, parent); err != nil {
			return err
		}
		if !o.Target.matches(actual) {
			return fmt.Errorf("value identity or scope does not match target")
		}
		list.Content = append(list.Content, n)
	case "merge":
		r := found[0]
		actual = r.Target
		for i := 0; i < len(o.Value.Content); i += 2 {
			if e.adapter.protectedCollection(o.Target.Kind, o.Value.Content[i].Value) {
				return fmt.Errorf("merge of resource collections is unsupported; target each resource explicitly")
			}
		}
		e.merge(r.Node, o.Value)
	default:
		r := found[0]
		actual = r.Target
		for i, n := range r.List.Content {
			if n != r.Node {
				continue
			}
			if o.Op == "remove" {
				r.List.Content = append(r.List.Content[:i], r.List.Content[i+1:]...)
			} else {
				r.List.Content[i] = e.clone(o.Value)
			}
			break
		}
	}
	if err := e.validate(); err != nil {
		return err
	}
	if o.Op == "merge" || o.Op == "replace" {
		// Exact identity comparison, not the selector's matches(): an omitted
		// instance_name is a wildcard when selecting but not when checking identity.
		kept := false
		for _, r := range e.resources {
			if r.Target.String() == actual.String() {
				kept = true
			}
		}
		if !kept {
			return fmt.Errorf("identity and scope are immutable; remove and add the resource")
		}
	}
	changed := []string{""}
	if o.Op == "merge" {
		changed = fields(o.Value, "")
	}
	e.record(o.Op, actual, changed, o.origin)
	e.recordCascade(previous, before, actual, o.origin)
	return nil
}

// recordCascade records nested resources that a whole-resource operation added,
// replaced, or removed, so deleted children remain explainable by identity.
func (e *engine) recordCascade(previous []*resource, before map[string][]byte, actual Target, origin source) {
	seen := map[string]bool{}
	for _, r := range e.resources {
		key := e.adapter.identityKey(r.Target)
		seen[key] = true
		if r.Target.String() == actual.String() {
			continue
		}
		old, existed := before[key]
		if !existed {
			e.record("add", r.Target, []string{""}, origin)
			continue
		}
		// Ancestors contain the changed child syntactically; their own fields did not change.
		if !isDescendant(r, actual) {
			continue
		}
		if now, _ := encode(r.Node); !bytes.Equal(old, now) {
			e.record("replace", r.Target, []string{""}, origin)
		}
	}
	for _, r := range previous {
		if !seen[e.adapter.identityKey(r.Target)] && r.Target.String() != actual.String() {
			e.record("remove", r.Target, []string{""}, origin)
		}
	}
}

func isDescendant(r *resource, t Target) bool {
	for p := r.Parent; p != nil; p = p.Parent {
		if p.Target.String() == t.String() {
			return true
		}
	}
	return false
}

func (e *engine) record(op string, t Target, paths []string, s source) {
	e.history = append(e.history, Event{len(e.history) + 1, e.display(s.File), s.Line, op, t, paths})
}

func (r *Result) Explain(t Target, field string) ([]Event, error) {
	if err := r.e.adapter.validateTarget(t); err != nil {
		return nil, err
	}
	parts, err := pointer(field)
	if err != nil {
		return nil, err
	}
	if len(parts) > 0 && r.e.adapter.protectedCollection(t.Kind, parts[0]) {
		return nil, fmt.Errorf("select the child resource directly to explain a resource collection")
	}
	identities := map[string]bool{}
	for _, event := range r.History {
		if t.matches(event.Target) {
			identities[event.Target.String()] = true
		}
	}
	if len(identities) != 1 {
		return nil, fmt.Errorf("target %s: expected 1, actual %d (including deleted resources)", t.String(), len(identities))
	}
	var events []Event
	for _, event := range r.History {
		if !t.matches(event.Target) {
			continue
		}
		var relevant []string
		for _, p := range event.Fields {
			if field == "" || p == "" || p == field || strings.HasPrefix(p, field+"/") || strings.HasPrefix(field, p+"/") {
				relevant = append(relevant, p)
			}
		}
		if len(relevant) > 0 {
			event.Fields = relevant
			events = append(events, event)
		}
	}
	if len(events) == 0 {
		return nil, fmt.Errorf("field %q: no composition history", field)
	}
	return events, nil
}
