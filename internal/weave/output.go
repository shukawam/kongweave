package weave

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// bundle holds all state of one publication. It copies provenance so that
// writing never mutates the engine, and a Result can be written repeatedly.
type bundle struct {
	e       *engine
	origins map[*yaml.Node]source
	files   map[string][]byte
}

type manifest struct {
	APIVersion string   `json:"apiVersion"`
	Format     string   `json:"format"`
	Config     string   `json:"config"`
	Files      []string `json:"files"`
	History    []Event  `json:"history"`
}

func (r *Result) Write(directory string) error {
	out, err := filepath.Abs(directory)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("output already exists; choose a new directory")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect output directory")
	}
	b := bundle{e: r.e, origins: map[*yaml.Node]source{}, files: map[string][]byte{}}
	root := cloneNode(r.e.root, r.e.origins, b.origins)
	// control_planes is a root-only collection, so its _deck blocks are found
	// without re-indexing the already validated tree.
	if cps := get(root, "control_planes"); cps != nil {
		for _, cp := range cps.Content {
			if err := b.rewriteDeck(get(cp, "_deck")); err != nil {
				return err
			}
		}
	}
	if err := b.rewrite(root); err != nil {
		return err
	}
	data, err := encode(root)
	if err != nil {
		return err
	}
	b.files["config.yaml"] = data
	m := manifest{"kongweave/v1alpha1", r.Format, "config.yaml", sortedKeys(b.files), r.History}
	metadata, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	b.files["manifest.json"] = append(metadata, '\n')
	parent := filepath.Dir(out)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return fmt.Errorf("cannot create output parent")
	}
	// A sibling staging directory keeps the final rename on one filesystem.
	// Existing bundles are immutable; no delete-and-replace window is necessary.
	stage, err := os.MkdirTemp(parent, ".kongweave-")
	if err != nil {
		return fmt.Errorf("cannot create staging directory")
	}
	defer os.RemoveAll(stage)
	for _, name := range sortedKeys(b.files) {
		dest := filepath.Join(stage, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			return fmt.Errorf("cannot stage asset directory")
		}
		if err := os.WriteFile(dest, b.files[name], 0600); err != nil {
			return fmt.Errorf("cannot stage output file")
		}
	}
	// Coordinate concurrent Kongweave writers without modifying a valid bundle.
	lock := out + ".kongweave-lock"
	f, err := os.OpenFile(lock, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("output is locked or its lock cannot be created")
	}
	f.Close()
	defer os.Remove(lock)
	if _, err := os.Lstat(out); err == nil {
		return fmt.Errorf("output already exists; choose a new directory")
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("cannot inspect output directory")
	}
	if err := os.Rename(stage, out); err != nil {
		return fmt.Errorf("cannot publish output directory")
	}
	return nil
}

func (b *bundle) rewrite(n *yaml.Node) error {
	if n.Tag != "!file" {
		for _, c := range n.Content {
			if err := b.rewrite(c); err != nil {
				return err
			}
		}
		return nil
	}
	if n.Kind == yaml.MappingNode {
		pathNode := get(n, "path")
		name, err := b.asset(pathNode.Value, b.origins[n], false)
		if err != nil {
			return err
		}
		pathNode.Value = name
		return nil
	}
	file, suffix, hasSuffix := strings.Cut(n.Value, "#")
	name, err := b.asset(file, b.origins[n], false)
	if err != nil {
		return err
	}
	n.Value = name
	if hasSuffix {
		n.Value += "#" + suffix
	}
	return nil
}

func (b *bundle) rewriteDeck(d *yaml.Node) error {
	if d == nil {
		return nil
	}
	if !plainMap(d) {
		return fmt.Errorf("_deck must be a mapping")
	}
	if err := keys(d, "files", "flags"); err != nil {
		return fmt.Errorf("_deck: %w", err)
	}
	if flags := get(d, "flags"); flags != nil && (flags.Kind != yaml.SequenceNode || flags.Tag != "!!seq" || len(flags.Content) != 0) {
		// Flags may carry paths of their own; copying only state files
		// would change those paths when the bundle is moved.
		return fmt.Errorf("non-empty _deck.flags are unsupported in portable bundles")
	}
	files := get(d, "files")
	if files == nil || files.Kind != yaml.SequenceNode || files.Tag != "!!seq" || len(files.Content) == 0 {
		return fmt.Errorf("_deck.files must be a non-empty array of local file paths")
	}
	for _, f := range files.Content {
		path, ok := textValue(f)
		if !ok {
			return fmt.Errorf("_deck.files requires plain string paths")
		}
		name, err := b.asset(path, b.origins[f], true)
		if err != nil {
			return err
		}
		f.Value = name
	}
	return nil
}

func (b *bundle) asset(ref string, s source, deck bool) (string, error) {
	fail := func(reason string) (string, error) {
		return "", fmt.Errorf("%s:%d: file reference: %s (value withheld)", b.e.display(s.File), s.Line, reason)
	}
	if strings.ContainsAny(ref, "*?[]$") {
		return fail("globs and dynamic paths are unsupported")
	}
	path, err := relativeFile(s.File, ref)
	if err != nil {
		return fail("expected a relative local file path")
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fail("cannot access asset")
	}
	base, err := filepath.EvalSymlinks(filepath.Dir(s.File))
	if err != nil {
		return fail("cannot access source directory")
	}
	rel, err := filepath.Rel(base, real)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fail("asset must stay within its declaring file's directory (including symlinks)")
	}
	data, err := readLocal(real)
	if err != nil {
		return fail(err.Error())
	}
	if deck {
		// A decK state file is copied as an opaque native artifact, but custom file
		// tags would require another resolver and cannot safely be relocated. The
		// same bytes are validated and stored, so the check cannot race the copy.
		n, err := parse(data, nil)
		if err != nil {
			return fail("_deck asset must be a single YAML mapping")
		}
		if value(n, "_format_version") != "3.0" {
			return fail("_deck asset requires _format_version 3.0")
		}
		if err := validateTags(n, "deck"); err != nil {
			return fail("custom tags in _deck assets are unsupported")
		}
	}
	sum := sha256.Sum256(data)
	ext := strings.ToLower(filepath.Ext(ref))
	for _, r := range ext {
		if r != '.' && (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return fail("unsupported asset extension")
		}
	}
	name := "assets/" + hex.EncodeToString(sum[:]) + ext
	b.files[name] = data
	return name, nil
}
