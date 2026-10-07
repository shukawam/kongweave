package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCLIOutputsAndFailure(t *testing.T) {
	dir := filepath.Join("..", "..", "examples", "deck", "overlays", "prod")
	out := filepath.Join(t.TempDir(), "out")
	var stdout, stderr bytes.Buffer
	if err := run([]string{"build", dir, "-o", out}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "Built deck bundle") {
		t.Fatal("build mixes logs and artifacts")
	}
	original, err := os.ReadFile(filepath.Join(out, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"build", dir, "-o", out}, &stdout, &stderr); err == nil {
		t.Fatal("overwrote existing bundle")
	}
	after, _ := os.ReadFile(filepath.Join(out, "config.yaml"))
	if !bytes.Equal(original, after) {
		t.Fatal("changed existing bundle")
	}
	stdout.Reset()
	stderr.Reset()
	if err := run([]string{"explain", dir, "--kind", "plugins", "--name", "rate-limiting", "--scope", "route=catalog-public", "--field", "/config/minute", "--json"}, &stdout, &stderr); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), `"operation": "merge"`) || stderr.Len() != 0 || strings.Contains(stdout.String(), "600") {
		t.Fatal("incorrect explain output")
	}
}

func TestCLIHelpAndUsage(t *testing.T) {
	for _, args := range [][]string{{}, {"--help"}, {"build", "--help"}, {"explain", "--help"}, {"build", "dir", "--help"}, {"explain", "dir", "--kind", "plugins", "-h"}} {
		var stdout, stderr bytes.Buffer
		if err := run(args, &stdout, &stderr); err != nil || !strings.Contains(stdout.String(), "Usage:") || stderr.Len() != 0 {
			t.Fatalf("%v: %v (stdout %d bytes, stderr %d bytes)", args, err, stdout.Len(), stderr.Len())
		}
	}
	var out bytes.Buffer
	if err := run([]string{"version"}, &out, &out); err != nil || out.Len() == 0 {
		t.Fatal(err)
	}
	// A flag value that looks like a help option is a value, not a help request.
	dir := filepath.Join("..", "..", "examples", "deck", "overlays", "prod")
	out.Reset()
	err := run([]string{"explain", dir, "--kind", "services", "--name", "--help"}, &out, &out)
	if err == nil || !strings.Contains(err.Error(), "expected 1, actual 0") || strings.Contains(out.String(), "Usage:") {
		t.Fatalf("value --help treated as help: %v / %q", err, out.String())
	}
	absDir, _ := filepath.Abs(dir)
	cwd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(cwd)
	out.Reset()
	if err := run([]string{"build", absDir, "--output", "-h"}, &out, &out); err != nil || strings.Contains(out.String(), "Usage:") {
		t.Fatalf("value -h treated as help: %v / %q", err, out.String())
	}
	if _, err := os.Stat(filepath.Join("-h", "config.yaml")); err != nil {
		t.Fatal("bundle was not written to the directory named -h")
	}
	for _, args := range [][]string{{"wat"}, {"build"}, {"build", "--output", "x"}, {"build", "x", "extra"}} {
		var out bytes.Buffer
		if err := run(args, &out, &out); err == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
