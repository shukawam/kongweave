package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shukawam/kongweave/internal/weave"
)

var version = "dev"

const help = `Kongweave — Environment overlays for decK and kongctl.

Usage:
  kongweave build <overlay-directory> [--output <new-directory>]
  kongweave explain <overlay-directory> --kind <kind> --name <name> [flags]
  kongweave explain <overlay-directory> --kind <kind> --ref <ref> [flags]
  kongweave version

Build writes config.yaml, assets, and a value-free manifest.json to a new
bundle directory (default: <overlay-directory>/build). Existing output is
never overwritten. Relative --output paths are relative to the caller.
Input references are always relative to the file declaring them.

Explain flags:
  --kind           Native plural resource kind, e.g. plugins or ai_gateway_models
  --name           decK name (username for consumers)
  --ref            kongctl ref
  --scope          Exact scope key=value; repeat for combined plugin scopes
  --global         Explicitly select an unscoped/global resource
  --instance-name  Optional decK plugin instance_name
  --field          JSON Pointer to a mapping field, e.g. /config/minute
  --json           Emit ordered, value-free JSON history

No network access, environment expansion, deployment, or native CLI is used.
`

type scopes map[string]string

func (s *scopes) String() string { return "" }
func (s *scopes) Set(v string) error {
	k, id, ok := strings.Cut(v, "=")
	if !ok || k == "" || id == "" {
		return fmt.Errorf("scope requires key=value")
	}
	if *s == nil {
		*s = scopes{}
	}
	if _, ok := (*s)[k]; ok {
		return fmt.Errorf("duplicate scope key")
	}
	(*s)[k] = id
	return nil
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "--help" || args[0] == "-h" || args[0] == "help" {
		fmt.Fprint(stdout, help)
		return nil
	}
	if args[0] == "version" {
		fmt.Fprintln(stdout, "kongweave "+version)
		return nil
	}
	command := args[0]
	if command != "build" && command != "explain" {
		return fmt.Errorf("unknown command; run kongweave --help")
	}
	if len(args) == 2 && (args[1] == "--help" || args[1] == "-h") {
		fmt.Fprint(stdout, help)
		return nil
	}
	if len(args) < 2 || strings.HasPrefix(args[1], "-") {
		return fmt.Errorf("provide overlay directory before flags; run kongweave --help")
	}
	directory := args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() { fmt.Fprint(stderr, help) }
	// Registered as ordinary flags so the parser decides whether "--help" is an
	// option or the value of a preceding flag such as --name.
	showHelp := false
	flags.BoolVar(&showHelp, "help", false, "show help")
	flags.BoolVar(&showHelp, "h", false, "show help")
	output := ""
	t := weave.Target{}
	field := ""
	asJSON := false
	global := false
	var scope scopes
	if command == "build" {
		flags.StringVar(&output, "output", "", "new output bundle directory")
		flags.StringVar(&output, "o", "", "new output bundle directory")
	} else {
		flags.StringVar(&t.Kind, "kind", "", "resource kind")
		flags.StringVar(&t.Name, "name", "", "decK name")
		flags.StringVar(&t.Ref, "ref", "", "kongctl ref")
		flags.StringVar(&t.InstanceName, "instance-name", "", "plugin instance_name")
		flags.Var(&scope, "scope", "exact scope key=value (repeatable)")
		flags.StringVar(&field, "field", "", "mapping field JSON Pointer")
		flags.BoolVar(&asJSON, "json", false, "JSON history")
		flags.BoolVar(&global, "global", false, "empty scope")
	}
	if err := flags.Parse(args[2:]); err != nil {
		return fmt.Errorf("invalid command flags")
	}
	if showHelp {
		fmt.Fprint(stdout, help)
		return nil
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	result, err := weave.Load(directory)
	if err != nil {
		return err
	}
	if command == "build" {
		if output == "" {
			output = filepath.Join(directory, "build")
		}
		if err := result.Write(output); err != nil {
			return err
		}
		fmt.Fprintf(stderr, "Built %s bundle: %s\n", result.Format, filepath.Join(output, "config.yaml"))
		return nil
	}
	if global && len(scope) > 0 {
		return fmt.Errorf("--global cannot be combined with --scope")
	}
	t.Scope = scope
	if global {
		t.Scope = map[string]string{}
	}
	events, err := result.Explain(t, field)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(events)
	}
	for _, event := range events {
		paths := strings.Join(event.Fields, ", ")
		if paths == "" {
			paths = "(whole resource)"
		}
		fmt.Fprintf(stdout, "%03d %s:%d %s %s fields=%s\n", event.Order, event.Source, event.Line, event.Operation, event.Target.String(), paths)
	}
	return nil
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
