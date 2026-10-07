package weave

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExamplesBuildAndExplain(t *testing.T) {
	for _, format := range []string{"deck", "kongctl"} {
		for _, env := range []string{"dev", "prod"} {
			t.Run(format+"/"+env, func(t *testing.T) {
				r := mustLoad(t, filepath.Join("..", "..", "examples", format, "overlays", env))
				out := filepath.Join(t.TempDir(), "bundle")
				if err := r.Write(out); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(filepath.Join(out, "config.yaml")); err != nil {
					t.Fatal(err)
				}
				target := Target{Kind: "plugins", Name: "rate-limiting", Scope: map[string]string{"route": "catalog-public"}}
				field := "/config/minute"
				if format == "kongctl" {
					target = Target{Kind: "ai_gateway_models", Ref: "example-chat", Scope: map[string]string{"ai_gateway": "example-ai"}}
					field = "/config/route/paths"
				}
				events, err := r.Explain(target, field)
				if err != nil || len(events) != 2 {
					t.Fatalf("history: %+v, %v", events, err)
				}
			})
		}
	}
}
