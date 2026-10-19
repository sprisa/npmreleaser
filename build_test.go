//go:build !windows

package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPublishPackagesMainLast(t *testing.T) {
	for _, scenario := range []string{"success", "platform-failure", "dry-run"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join(root, "publish.log")
			bin := filepath.Join(root, "bin")
			if err := os.Mkdir(bin, 0755); err != nil {
				t.Fatal(err)
			}
			script := `#!/bin/sh
name="${PWD##*/}"
printf '%s %s\n' "$name" "$*" >> "$PUBLISH_LOG"
if [ "$name" = "$FAIL_PACKAGE" ]; then exit 9; fi
`
			if err := os.WriteFile(filepath.Join(bin, "npm"), []byte(script), 0755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("PUBLISH_LOG", log)
			t.Setenv("FAIL_PACKAGE", "")
			if scenario == "platform-failure" {
				t.Setenv("FAIL_PACKAGE", "platform-two")
			}
			builds := []PlatformBuild{{pkgDir: filepath.Join(root, "platform-one")}, {pkgDir: filepath.Join(root, "platform-two")}}
			mainDir := filepath.Join(root, "main")
			for _, path := range []string{builds[0].pkgDir, builds[1].pkgDir, mainDir} {
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			err := publishPackages(context.Background(), builds, mainDir, scenario == "dry-run")
			data, readErr := os.ReadFile(log)
			if readErr != nil {
				t.Fatal(readErr)
			}
			args := "publish"
			if scenario == "dry-run" {
				args += " --dry-run"
			}
			want := "platform-one " + args + "\nplatform-two " + args + "\n"
			if scenario == "platform-failure" {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.ExitCode() != 9 || !strings.Contains(err.Error(), "platform-two") {
					t.Fatalf("lost platform failure: %v", err)
				}
			} else {
				want += "main " + args + "\n"
				if err != nil {
					t.Fatal(err)
				}
			}
			if string(data) != want {
				t.Fatalf("publish order: %q, want %q", data, want)
			}
		})
	}
}

func TestLauncherErrorsAndChildStatus(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("Node.js is required to execute generated launchers")
	}
	nodeOS, err := goOsToNode(runtime.GOOS)
	if err != nil {
		t.Fatal(err)
	}
	nodeArch, err := goArchToNode(runtime.GOARCH)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	build := PlatformBuild{pkgName: "example_platform", binName: "example-bin", nodeOs: nodeOS, nodeArch: nodeArch}
	launcher := filepath.Join(root, "example")
	if err := os.WriteFile(launcher, []byte(renderLauncher([]PlatformBuild{build})), 0755); err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command(node, launcher).CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 ||
		!strings.Contains(string(output), "Cannot find module") ||
		strings.Contains(string(output), "console.err is not a function") {
		t.Fatalf("missing platform package: %v\n%s", err, output)
	}
	packageDir := filepath.Join(root, "node_modules", build.pkgName)
	if err := os.MkdirAll(packageDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "package.json"), []byte(`{"name":"example_platform","version":"1.0.0"}`), 0644); err != nil {
		t.Fatal(err)
	}
	child := "#!/usr/bin/env node\nconsole.log(JSON.stringify(process.argv.slice(2)));\nprocess.exit(23);\n"
	if err := os.WriteFile(filepath.Join(packageDir, build.binName), []byte(child), 0755); err != nil {
		t.Fatal(err)
	}
	output, err = exec.Command(node, launcher, "--example", "argument with spaces").CombinedOutput()
	if !errors.As(err, &exit) || exit.ExitCode() != 23 ||
		!strings.Contains(string(output), `["--example","argument with spaces"]`) {
		t.Fatalf("child arguments/status: %v\n%s", err, output)
	}
}
