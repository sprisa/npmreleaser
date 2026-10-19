package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigEnvironmentTemplates(t *testing.T) {
	t.Setenv("RELEASER_TEST_VERSION", "1.2.3")
	t.Setenv("RELEASER_TEST_NAME", "example-cli")
	t.Setenv("RELEASER_TEST_OS", "linux")
	t.Setenv("RELEASER_TEST_TEXT", "a quoted \"value\"\nwith a newline and \\backslash")
	path := filepath.Join(t.TempDir(), "npmreleaser.json")
	data := `{
  "package.json": {
    "name": "{{ .Env.RELEASER_TEST_NAME }}",
    "version": "{{.Env.RELEASER_TEST_VERSION}}",
    "description": "prefix: {{ .Env.RELEASER_TEST_TEXT }}",
    "custom": {"enabled": true, "count": 4, "values": ["literal", "{{ .Env.RELEASER_TEST_TEXT }}"]}
  },
  "bin": "{{ .Env.RELEASER_TEST_NAME }}",
  "gobuild": "go build -ldflags=-X=main.version={{ .Env.RELEASER_TEST_VERSION }}",
  "files": ["literal.txt", "{{ .Env.RELEASER_TEST_NAME }}.md"],
  "platforms": ["{{ .Env.RELEASER_TEST_OS }}/amd64"]
}`
	if err := os.WriteFile(path, []byte(data), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Pkg["version"] != "1.2.3" || cfg.Pkg["name"] != "example-cli" || cfg.Bin != "example-cli" ||
		cfg.GoBuildCmd != "go build -ldflags=-X=main.version=1.2.3" || cfg.Platforms[0] != "linux/amd64" || cfg.Files[1] != "example-cli.md" {
		t.Fatalf("templates not rendered: %+v", cfg)
	}
	want := os.Getenv("RELEASER_TEST_TEXT")
	custom := cfg.Pkg["custom"].(map[string]any)
	if cfg.Pkg["description"] != "prefix: "+want || custom["values"].([]any)[1] != want ||
		custom["enabled"] != true || custom["count"] != float64(4) {
		t.Fatalf("JSON values changed: %#v", cfg.Pkg)
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != data {
		t.Fatal("loading templates modified the source configuration")
	}
}

func TestConfigTemplateErrors(t *testing.T) {
	const missing = "RELEASER_TEST_VARIABLE_NOT_SET"
	t.Setenv(missing, "temporary")
	if err := os.Unsetenv(missing); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, data, want string }{
		{"missing-variable", `{"package.json":{"version":"{{ .Env.RELEASER_TEST_VARIABLE_NOT_SET }}"}}`, "package.json.version"},
		{"malformed-template", `{"bin":"{{ .Env.VERSION }"}`, ".bin"},
		{"invalid-json", `{"package.json":`, "parse"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "npmreleaser.json")
			if err := os.WriteFile(path, []byte(tc.data), 0644); err != nil {
				t.Fatal(err)
			}
			if _, err := loadConfig(path); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("wanted %q error, got %v", tc.want, err)
			}
		})
	}
}

func TestConfigLiteralValuesRemainSupported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "npmreleaser.json")
	if err := os.WriteFile(path, []byte(`{"package.json":{"name":"literal","version":"0.1.0"},"bin":"literal","platforms":["linux/amd64"]}`), 0644); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path)
	if err != nil || cfg.Pkg["version"] != "0.1.0" || cfg.Bin != "literal" || cfg.Platforms[0] != "linux/amd64" {
		t.Fatalf("literal configuration changed: %+v, %v", cfg, err)
	}
}
