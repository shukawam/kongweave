package weave

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type Target struct {
	Kind         string            `json:"kind"`
	Name         string            `json:"name,omitempty"`
	Ref          string            `json:"ref,omitempty"`
	InstanceName string            `json:"instance_name,omitempty"`
	Scope        map[string]string `json:"scope"`
}

// String distinguishes an omitted scope (search every scope) from scope: {}
// (global), so diagnostics show what the selector actually meant.
func (t Target) String() string {
	if t.Scope == nil {
		b, _ := json.Marshal(struct {
			Kind         string `json:"kind"`
			Name         string `json:"name,omitempty"`
			Ref          string `json:"ref,omitempty"`
			InstanceName string `json:"instance_name,omitempty"`
		}{t.Kind, t.Name, t.Ref, t.InstanceName})
		return string(b)
	}
	b, _ := json.Marshal(t)
	return string(b)
}

func (t Target) matches(actual Target) bool {
	if t.Kind != actual.Kind || t.Name != "" && t.Name != actual.Name || t.Ref != "" && t.Ref != actual.Ref || t.InstanceName != "" && t.InstanceName != actual.InstanceName {
		return false
	}
	if t.Scope != nil {
		if len(t.Scope) != len(actual.Scope) {
			return false
		}
		for k, v := range t.Scope {
			if actual.Scope[k] != v {
				return false
			}
		}
	}
	return true
}

type resource struct {
	Target Target
	Node   *yaml.Node
	List   *yaml.Node
	Parent *resource
}

type resourceType struct{ Kind, Identity, Parent, Nested, Scope string }

type adapter struct {
	Format string
	Types  []resourceType
}

// scopeKinds maps a scope key to the collection kind its identifier refers to.
var scopeKinds = map[string]string{
	"service": "services", "route": "routes", "consumer": "consumers",
	"api": "apis", "portal": "portals", "ai_gateway": "ai_gateways",
}

// pluginScopes are the decK plugin binding fields; a plugin may combine them.
var pluginScopes = []string{"service", "route", "consumer"}

func scopeKey(kind string) string {
	for k, v := range scopeKinds {
		if v == kind {
			return k
		}
	}
	return ""
}

func isPluginScope(key string) bool {
	for _, s := range pluginScopes {
		if s == key {
			return true
		}
	}
	return false
}

func (a adapter) deckPlugin(kind string) bool { return a.Format == "deck" && kind == "plugins" }

// immutableField reports whether key is part of a resource's identity or scope.
// Changing it would rename or move the resource; the contract requires remove/add.
func (a adapter) immutableField(kind, key string) bool {
	d, _ := a.descriptor(kind)
	if key == d.Identity || d.Scope != "" && key == d.Scope {
		return true
	}
	return a.deckPlugin(kind) && (isPluginScope(key) || key == "instance_name")
}

func newAdapter(format string) (adapter, error) {
	a := adapter{Format: format}
	switch format {
	case "deck":
		a.Types = []resourceType{
			{Kind: "services", Identity: "name"},
			{Kind: "routes", Identity: "name", Parent: "services", Nested: "routes", Scope: "service"},
			{Kind: "consumers", Identity: "username"},
			{Kind: "plugins", Identity: "name"},
		}
	case "kongctl":
		a.Types = []resourceType{
			{Kind: "portals", Identity: "ref"}, {Kind: "apis", Identity: "ref"},
			{Kind: "control_planes", Identity: "ref"}, {Kind: "application_auth_strategies", Identity: "ref"},
			{Kind: "ai_gateways", Identity: "ref"},
			{Kind: "portal_pages", Identity: "ref", Parent: "portals", Nested: "pages", Scope: "portal"},
			{Kind: "api_versions", Identity: "ref", Parent: "apis", Nested: "versions", Scope: "api"},
			{Kind: "api_documents", Identity: "ref", Parent: "apis", Nested: "documents", Scope: "api"},
			{Kind: "api_publications", Identity: "ref", Parent: "apis", Nested: "publications", Scope: "api"},
			{Kind: "ai_gateway_models", Identity: "ref", Parent: "ai_gateways", Nested: "models", Scope: "ai_gateway"},
			{Kind: "ai_gateway_model_providers", Identity: "ref", Parent: "ai_gateways", Nested: "model_providers", Scope: "ai_gateway"},
			{Kind: "ai_gateway_policies", Identity: "ref", Parent: "ai_gateways", Nested: "policies", Scope: "ai_gateway"},
		}
	default:
		return a, fmt.Errorf("format must be deck or kongctl")
	}
	return a, nil
}

func (a adapter) descriptor(kind string) (resourceType, bool) {
	for _, t := range a.Types {
		if t.Kind == kind {
			return t, true
		}
	}
	return resourceType{}, false
}

func (a adapter) children(kind string) []resourceType {
	var out []resourceType
	for _, t := range a.Types {
		if t.Parent == kind {
			out = append(out, t)
		}
	}
	if a.Format == "deck" && scopeKey(kind) != "" {
		out = append(out, resourceType{Kind: "plugins", Identity: "name", Parent: kind, Nested: "plugins"})
	}
	return out
}

func (a adapter) protectedCollection(kind, key string) bool {
	for _, t := range a.children(kind) {
		if t.Nested == key {
			return true
		}
	}
	for _, k := range a.unsupportedChildren(kind) {
		if key == k {
			return true
		}
	}
	return false
}

func (a adapter) unsupportedChildren(kind string) []string {
	if a.Format == "deck" {
		if kind == "consumers" {
			return []string{"acls", "basicauth_credentials", "hmacauth_credentials", "jwt_secrets", "keyauth_credentials", "oauth2_credentials", "mtls_auth_credentials", "groups"}
		}
		return nil
	}
	switch kind {
	case "portals":
		return []string{"snippets", "customization", "custom_domain", "email_config", "email_templates", "identity_providers", "teams", "roles", "developers", "applications", "audit_log_webhook"}
	case "portal_pages":
		return []string{"children"}
	case "apis":
		return []string{"implementations"}
	case "api_documents":
		return []string{"children"}
	case "control_planes":
		return []string{"gateway_services", "dataplane_certificates", "data_plane_certificates"}
	case "ai_gateways":
		return []string{"agents", "auth_strategies", "identity_providers", "ca_certificates", "certificates", "config_stores", "consumer_groups", "consumers", "custom_policies", "data_plane_certificates", "mcp_servers", "snis", "vaults"}
	}
	return nil
}

func (a adapter) validateTarget(t Target) error {
	d, ok := a.descriptor(t.Kind)
	if !ok {
		return fmt.Errorf("unsupported resource kind %q for %s", t.Kind, a.Format)
	}
	if a.Format == "deck" {
		if t.Name == "" || t.Ref != "" {
			return fmt.Errorf("decK targets require name (username for consumers), not ref")
		}
		if t.InstanceName != "" && t.Kind != "plugins" {
			return fmt.Errorf("instance_name is supported only for plugins")
		}
	} else if t.Ref == "" || t.Name != "" || t.InstanceName != "" {
		return fmt.Errorf("kongctl targets require ref, not name or instance_name")
	}
	for k, v := range t.Scope {
		valid := k == d.Scope && d.Scope != ""
		if a.deckPlugin(t.Kind) {
			valid = isPluginScope(k)
		}
		if !valid || v == "" {
			return fmt.Errorf("unsupported or empty scope for %s", t.Kind)
		}
	}
	return nil
}

func relation(n *yaml.Node, format, key string) (string, error) {
	if n == nil || n.Tag == "!!null" {
		return "", nil
	}
	if s, ok := textValue(n); ok && s != "" {
		return s, nil
	}
	if format == "kongctl" && n.Tag == "!ref" && n.Kind == yaml.ScalarNode {
		return strings.SplitN(n.Value, "#", 2)[0], nil
	}
	if format == "deck" && plainMap(n) {
		field := "name"
		if key == "consumer" {
			field = "username"
		}
		if len(n.Content) == 2 && value(n, field) != "" {
			return value(n, field), nil
		}
	}
	alternative := "name/username mapping"
	if format == "kongctl" {
		alternative = "scalar !ref"
	}
	return "", fmt.Errorf("unsupported parent/scope reference; use a literal stable identifier or %s", alternative)
}

// parentID is the identifier a child must reference to belong to parent.
func (a adapter) parentID(parent *resource) string {
	if a.Format == "deck" {
		return parent.Target.Name
	}
	return parent.Target.Ref
}

// reconcile returns the scope identifier for key, given the value declared in
// the resource and the enclosing parent, if any. Nesting wins; a conflicting
// declaration is an error rather than a silent move.
func (a adapter) reconcile(declared string, parent *resource) (string, error) {
	if parent == nil {
		return declared, nil
	}
	if declared != "" && declared != a.parentID(parent) {
		return "", fmt.Errorf("parent scope conflicts with nesting")
	}
	return a.parentID(parent), nil
}

func (a adapter) identify(t resourceType, n *yaml.Node, parent *resource) (Target, error) {
	target := Target{Kind: t.Kind, Scope: map[string]string{}}
	id, ok := textValue(get(n, t.Identity))
	if !ok || id == "" {
		return target, fmt.Errorf("%s requires an explicit non-empty string %s; deferred identities are unsupported", t.Kind, t.Identity)
	}
	if a.Format == "deck" {
		target.Name = id
	} else {
		target.Ref = id
	}
	if t.Scope != "" {
		s, err := relation(get(n, t.Scope), a.Format, t.Scope)
		if err != nil {
			return target, err
		}
		if s, err = a.reconcile(s, parent); err != nil {
			return target, err
		}
		if s != "" {
			target.Scope[t.Scope] = s
		} else if a.Format == "kongctl" {
			return target, fmt.Errorf("%s requires a parent scope", t.Kind)
		}
	}
	if a.deckPlugin(t.Kind) {
		if get(n, "consumer_group") != nil {
			return target, fmt.Errorf("consumer_group plugin scopes are unsupported")
		}
		if p := get(n, "instance_name"); p != nil {
			v, ok := textValue(p)
			if !ok || v == "" {
				return target, fmt.Errorf("instance_name must be a non-empty string")
			}
			target.InstanceName = v
		}
		for _, k := range pluginScopes {
			s, err := relation(get(n, k), a.Format, k)
			if err != nil {
				return target, err
			}
			if parent != nil && scopeKey(parent.Target.Kind) == k {
				if s, err = a.reconcile(s, parent); err != nil {
					return target, fmt.Errorf("plugin scope conflicts with nesting")
				}
			}
			if s != "" {
				target.Scope[k] = s
			}
		}
	}
	return target, nil
}

func (a adapter) index(root *yaml.Node) ([]*resource, error) {
	if a.Format == "deck" {
		v, ok := textValue(get(root, "_format_version"))
		if !ok || v != "3.0" {
			return nil, fmt.Errorf("decK requires _format_version: \"3.0\"")
		}
	}
	for i := 0; i < len(root.Content); i += 2 {
		k := root.Content[i].Value
		if _, ok := a.descriptor(k); ok {
			continue
		}
		if a.Format == "deck" && (k == "_format_version" || k == "_transform" || k == "_info" || k == "_workspace" || k == "_konnect") {
			continue
		}
		if a.Format == "kongctl" && k == "_defaults" {
			continue
		}
		return nil, fmt.Errorf("unsupported top-level key %q for %s", k, a.Format)
	}
	if a.Format == "kongctl" {
		var reject func(*yaml.Node) error
		reject = func(n *yaml.Node) error {
			if n.Kind == yaml.MappingNode {
				for i := 0; i < len(n.Content); i += 2 {
					if n.Content[i].Value == "_templates" || n.Content[i].Value == "_extends" {
						return fmt.Errorf("_templates and _extends are unsupported; supply explicitly declared resources")
					}
				}
			}
			for _, c := range n.Content {
				if err := reject(c); err != nil {
					return err
				}
			}
			return nil
		}
		if err := reject(root); err != nil {
			return nil, err
		}
	}
	var out []*resource
	var walk func(resourceType, *yaml.Node, *resource) error
	walk = func(t resourceType, list *yaml.Node, parent *resource) error {
		if list == nil {
			return nil
		}
		if list.Kind != yaml.SequenceNode || list.Tag != "!!seq" {
			return fmt.Errorf("%s must be a resource array (null and tagged collections are unsupported)", t.Kind)
		}
		for _, n := range list.Content {
			if !plainMap(n) {
				return fmt.Errorf("%s entries must be plain mappings", t.Kind)
			}
			target, err := a.identify(t, n, parent)
			if err != nil {
				return err
			}
			r := &resource{target, n, list, parent}
			out = append(out, r)
			for _, key := range a.unsupportedChildren(t.Kind) {
				if get(n, key) != nil {
					return fmt.Errorf("unsupported resource structure %s.%s", t.Kind, key)
				}
			}
			for _, child := range a.children(t.Kind) {
				if err := walk(child, get(n, child.Nested), r); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Source order, including collection order, remains observable and deterministic.
	for i := 0; i < len(root.Content); i += 2 {
		if t, ok := a.descriptor(root.Content[i].Value); ok {
			if err := walk(t, root.Content[i+1], nil); err != nil {
				return nil, err
			}
		}
	}
	counts := map[string]int{}
	instances := map[string]int{}
	for _, r := range out {
		counts[a.identityKey(r.Target)]++
		if r.Target.InstanceName != "" {
			instances[r.Target.InstanceName]++
		}
	}
	for _, r := range out {
		if count := counts[a.identityKey(r.Target)]; count > 1 {
			return nil, fmt.Errorf("duplicate identifier: target %s: expected 1, actual %d", r.Target.String(), count)
		}
		if count := instances[r.Target.InstanceName]; r.Target.InstanceName != "" && count > 1 {
			return nil, fmt.Errorf("duplicate instance_name: target %s: expected 1, actual %d", r.Target.String(), count)
		}
	}
	// A name-only relationship must resolve in the same document; UUID-only inputs
	// cannot silently turn a scoped plugin into a global plugin.
	for _, r := range out {
		for _, key := range sortedKeys(r.Target.Scope) {
			id := r.Target.Scope[key]
			kind := scopeKinds[key]
			count := 0
			for _, p := range out {
				if p.Target.Kind == kind && (a.Format == "deck" && p.Target.Name == id || a.Format == "kongctl" && p.Target.Ref == id) {
					count++
				}
			}
			if count != 1 {
				return nil, fmt.Errorf("target %s: parent scope %s: expected 1, actual %d", r.Target.String(), key, count)
			}
		}
	}
	return out, nil
}

func (a adapter) identityKey(t Target) string {
	if a.Format == "kongctl" {
		return t.Ref
	}
	if t.Kind != "plugins" {
		return t.Kind + ":" + t.Name
	}
	return t.String()
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func matches(rs []*resource, t Target) []*resource {
	var found []*resource
	for _, r := range rs {
		if t.matches(r.Target) {
			found = append(found, r)
		}
	}
	return found
}

func (a adapter) addList(root *yaml.Node, rs []*resource, t Target, n *yaml.Node) (*yaml.Node, *resource, error) {
	d, _ := a.descriptor(t.Kind)
	var parent *resource
	key := t.Kind
	if a.deckPlugin(t.Kind) {
		if t.Scope == nil {
			return nil, nil, fmt.Errorf("adding a plugin requires explicit scope; use scope: {} for global")
		}
		if len(t.Scope) == 1 {
			for s, id := range t.Scope {
				for _, r := range rs {
					if r.Target.Kind == scopeKinds[s] && r.Target.Name == id {
						parent = r
					}
				}
			}
			key = "plugins"
		} else {
			// Fixed order: Go map iteration would otherwise vary the emitted
			// field order between builds of identical input.
			for _, s := range sortedKeys(t.Scope) {
				if get(n, s) == nil {
					put(n, s, str(t.Scope[s]))
				}
			}
		}
	} else if d.Parent != "" && len(t.Scope) > 0 {
		for _, r := range rs {
			if r.Target.Kind == d.Parent && (r.Target.Ref == t.Scope[d.Scope] || a.Format == "deck" && r.Target.Name == t.Scope[d.Scope]) {
				parent = r
			}
		}
		key = d.Nested
	}
	if key != t.Kind && parent == nil {
		return nil, nil, fmt.Errorf("add parent: expected 1, actual 0")
	}
	container := root
	if parent != nil {
		container = parent.Node
	}
	list := get(container, key)
	if list == nil {
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		put(container, key, list)
	}
	if list.Kind != yaml.SequenceNode {
		return nil, nil, fmt.Errorf("add destination is not a resource array")
	}
	return list, parent, nil
}
