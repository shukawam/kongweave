package weave

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

func custom(n *yaml.Node) bool {
	return n != nil && n.Tag != "" && !strings.HasPrefix(n.Tag, "!!")
}

func validateTags(n *yaml.Node, format string) error {
	if custom(n) {
		if format != "kongctl" {
			return fmt.Errorf("custom YAML tags are unsupported in decK input (line %d)", n.Line)
		}
		switch n.Tag {
		case "!ref":
			ref, field, hasField := strings.Cut(n.Value, "#")
			if n.Kind != yaml.ScalarNode || ref == "" || hasField && field == "" {
				return fmt.Errorf("!ref requires a non-empty scalar ref or ref#field")
			}
		case "!file", "!env":
			if n.Kind == yaml.ScalarNode {
				identifier, _, _ := strings.Cut(n.Value, "#")
				if identifier == "" {
					return fmt.Errorf("%s requires a non-empty scalar", n.Tag)
				}
			} else if n.Kind == yaml.MappingNode {
				key := "path"
				if n.Tag == "!env" {
					key = "var"
				}
				if err := tagKeys(n, key, "extract"); err != nil {
					return err
				}
				if value(n, key) == "" {
					return fmt.Errorf("%s requires a plain string %s", n.Tag, key)
				}
				if x := get(n, "extract"); x != nil {
					if _, ok := textValue(x); !ok {
						return fmt.Errorf("%s extract must be a plain string", n.Tag)
					}
				}
			} else {
				return fmt.Errorf("%s requires a scalar or mapping", n.Tag)
			}
			if hasNestedTag(n) {
				return fmt.Errorf("nested tags in %s are unsupported", n.Tag)
			}
		case "!lookup", "!external":
			if n.Kind == yaml.ScalarNode {
				k, v, ok := strings.Cut(n.Value, ":")
				if !ok || k == "" || v == "" {
					return fmt.Errorf("lookup scalar requires field:value")
				}
			} else if n.Kind == yaml.MappingNode && len(n.Content) > 0 {
				if get(n, "id") != nil && len(n.Content) != 2 {
					return fmt.Errorf("lookup id cannot be combined with other selectors")
				}
				for i := 1; i < len(n.Content); i += 2 {
					v := n.Content[i]
					if v.Tag != "!env" {
						if _, ok := textValue(v); !ok {
							return fmt.Errorf("lookup values must be strings or direct !env expressions")
						}
					}
				}
			} else {
				return fmt.Errorf("lookup requires field:value or a non-empty mapping")
			}
		case "!secret":
			if err := tagKeys(n, "source", "parts"); err != nil {
				return err
			}
			if len(n.Content) != 2 {
				return fmt.Errorf("!secret requires exactly one of source or parts")
			}
			if s := get(n, "source"); s != nil {
				if s.Tag != "!env" && s.Tag != "!file" {
					return fmt.Errorf("!secret source must be !env or !file")
				}
			} else {
				parts := get(n, "parts")
				if parts == nil || parts.Kind != yaml.SequenceNode || parts.Tag != "!!seq" || len(parts.Content) == 0 {
					return fmt.Errorf("!secret parts must be a non-empty array")
				}
				deferred := false
				for _, p := range parts.Content {
					if p.Tag == "!env" || p.Tag == "!file" {
						deferred = true
					} else if _, ok := textValue(p); !ok {
						return fmt.Errorf("!secret parts must be plain strings, !env, or !file")
					}
				}
				if !deferred {
					return fmt.Errorf("!secret parts require at least one deferred source")
				}
			}
		default:
			return fmt.Errorf("unsupported custom YAML tag at line %d", n.Line)
		}
	}
	for _, c := range n.Content {
		if err := validateTags(c, format); err != nil {
			return err
		}
	}
	return nil
}

func tagKeys(n *yaml.Node, allowed ...string) error {
	if n.Kind != yaml.MappingNode {
		return fmt.Errorf("%s requires a mapping", n.Tag)
	}
	if err := allowedKeys(n, allowed...); err != nil {
		return fmt.Errorf("%s expression: %w", n.Tag, err)
	}
	return nil
}

func hasNestedTag(n *yaml.Node) bool {
	for _, c := range n.Content {
		if custom(c) || hasNestedTag(c) {
			return true
		}
	}
	return false
}
