package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/google/shlex"
	"github.com/samber/lo"
	"github.com/sprisa/x/errutil"
	l "github.com/sprisa/x/log"
	"github.com/urfave/cli/v3"
	"golang.org/x/sync/errgroup"
)

var BuildCommand = &cli.Command{
	Name: "build",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "out-dir",
			Usage:   "Output directory",
			Aliases: []string{"o"},
			Value:   "dist/npm",
		},
		&cli.BoolFlag{
			Name:  "clean",
			Usage: "Clean out dir before running",
		},
		&cli.BoolFlag{
			Name:  "publish",
			Usage: "Publish all packages with npm publish",
		},
		&cli.BoolFlag{
			Name:  "dry-run",
			Usage: "Dry run publish",
		},
	},
	Action: func(ctx context.Context, c *cli.Command) error {
		var (
			outDir        = c.String("out-dir")
			shouldClean   = c.Bool("clean")
			shouldPublish = c.Bool("publish")
			dryPublish    = c.Bool("dry-run")
		)

		file, err := os.Open("npmreleaser.json")
		if err != nil {
			return errutil.WrapErr(err, "error finding npmreleaser.json")
		}
		defer file.Close()

		dec := json.NewDecoder(file)
		cfg := &ConfigSpec{}
		err = dec.Decode(cfg)
		if err != nil {
			return errutil.WrapErr(err, "error parsing npmreleaser.json")
		}

		pkgFiles := mapset.NewThreadUnsafeSet(
			"README.md",
			"LICENSE",
		)
		if len(cfg.Files) > 0 {
			for _, file := range cfg.Files {
				pkgFiles.Add(file)
			}
		}

		// Validations
		if cfg.Bin == "" {
			return errors.New("bin is missing in npmreleaser.json")
		}
		if len(cfg.Platforms) == 0 {
			return errors.New("platforms is missing in npmreleaser.json")
		}
		if len(cfg.Pkg) == 0 {
			return errors.New("package.json is missing in npmreleaser.json")
		}
		pkgName, ok := cfg.Pkg["name"].(string)
		if !ok {
			return errors.New("package.json name is required")
		}
		version, ok := cfg.Pkg["version"].(string)
		if !ok {
			return errors.New("package.json version is required")
		}

		builds := make([]PlatformBuild, 0, len(cfg.Platforms))
		for _, platform := range cfg.Platforms {
			split := strings.Split(platform, "/")
			if len(split) != 2 {
				return fmt.Errorf("invalid platform `%s`. It should follow os/arch (e.g. linux/amd64)", platform)
			}
			os := split[0]
			arch := split[1]
			nodeOs, err := goOsToNode(os)
			if err != nil {
				return err
			}
			nodeArch, err := goArchToNode(arch)
			if err != nil {
				return err
			}

			binName := fmt.Sprintf("%s-%s-%s", filepath.Base(pkgName), os, arch)
			if os == "windows" {
				binName += ".exe"
			}

			pkgName := fmt.Sprintf("%s_%s-%s", pkgName, os, arch)
			builds = append(builds, PlatformBuild{
				pkgName:  pkgName,
				binName:  binName,
				pkgDir:   filepath.Join(outDir, pkgName),
				goos:     os,
				goarch:   arch,
				nodeOs:   nodeOs,
				nodeArch: nodeArch,
			})
		}

		err = os.MkdirAll(outDir, 0700)
		if err != nil {
			return errutil.WrapErr(err, "error creating out dir")
		}

		// Build individual packages
		l.Log.Info().Msgf("Building %s@v%s for %d platforms", pkgName, version, len(cfg.Platforms))
		if shouldClean {
			err = os.RemoveAll(outDir)
			if err != nil {
				return errutil.WrapErr(err, "error cleaning out dir")
			}
		}

		goBuildCmd := "go build"
		if cfg.GoBuildCmd != "" {
			goBuildCmd = cfg.GoBuildCmd
		}

		group, gCtx := errgroup.WithContext(ctx)
		for _, build := range builds {
			pkgDir := filepath.Join(outDir, build.pkgName)

			err := os.MkdirAll(pkgDir, 0700)
			if err != nil {
				return errutil.WrapErr(err, "error create dir `%s`", pkgDir)
			}
			group.Go(func() error {
				binPath := filepath.Join(pkgDir, build.binName)
				// Go Build
				cmd := newCmd(
					gCtx,
					goBuildCmd,
					"-o", binPath,
				)
				cmd.Env = os.Environ()
				cmd.Env = append(
					cmd.Env,
					"GOOS="+build.goos,
					"GOARCH="+build.goarch,
				)
				// l.Log.Info().Msg(cmd.String())
				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("Error building %s/%s: %s", build.goos, build.goarch, string(out))
				}

				// Copy files
				for _, name := range pkgFiles.ToSlice() {
					src, err := os.Open(name)
					if err != nil {
						// Don't report errors for default files
						if name == "README.md" || name == "LICENSE" {
							continue
						}
						l.Log.Warn().Err(err).Msgf("unable to read file: %s", name)
						continue
					}
					defer src.Close()

					dst, err := os.Create(filepath.Join(pkgDir, name))
					if err != nil {
						l.Log.Err(err).Msgf("unable to write file: %s", name)
						continue
					}
					defer dst.Close()

					_, err = io.Copy(dst, src)
					if err != nil {
						l.Log.Err(err).Msgf("unable to copy file: %s", name)
						continue
					}
				}

				// Create package.json
				pkgJson := maps.Clone(cfg.Pkg)
				pkgJson["name"] = build.pkgName
				// Fix for yarn
				pkgJson["preferUnplugged"] = true
				pkgJson["files"] = append(pkgFiles.ToSlice(), build.binName)
				pkgJson["os"] = []string{build.nodeOs}
				pkgJson["cpu"] = []string{build.nodeArch}
				delete(pkgJson, "bin")

				pkgFile, err := os.Create(
					filepath.Join(pkgDir, "package.json"),
				)
				if err != nil {
					return errutil.WrapErr(err, "error writing package.json")
				}
				defer pkgFile.Close()

				enc := json.NewEncoder(pkgFile)
				enc.SetEscapeHTML(false)
				enc.SetIndent("", "  ")
				err = enc.Encode(pkgJson)
				if err != nil {
					return errutil.WrapErr(err, "error building package.json")
				}

				l.Log.Info().Msgf("Done: %s/%s", build.goos, build.goarch)
				return nil
			})
		}
		err = group.Wait()
		if err != nil {
			return err
		}

		// Build main package
		binFileName := filepath.Base(pkgName)
		binFile := fmt.Sprintf(`
#!/usr/bin/env node

const { execFileSync } = require('child_process');

const platform = process.platform + '/' + process.arch;
switch (platform) {
  %s
  default:
    throw new Error('Unsupported platform: ' + platform)
}`,
			strings.Join(
				lo.Map(builds, func(build PlatformBuild, _ int) string {
					return fmt.Sprintf(`
	case "%s": {
		try {
			execFileSync(
				require.resolve('%s/package.json').replace('package.json', '%s'),
				process.argv.slice(2),
				{ stdio: "inherit" }
			)
		} catch(err) {
		 	if (typeof err.status === "number") {
				process.exit(err.status);
			}
			console.err(err)
		}
		break;
	}`,
						fmt.Sprintf("%s/%s", build.nodeOs, build.nodeArch),
						build.pkgName,
						build.binName,
					)
				}),
				"  ",
			),
		)
		binFile = strings.TrimSpace(binFile)

		pkgDir := filepath.Join(outDir, pkgName)
		err = os.MkdirAll(pkgDir, 0700)
		if err != nil {
			return errutil.WrapErr(err, "error creating root package dir")
		}

		err = os.WriteFile(
			filepath.Join(pkgDir, binFileName),
			[]byte(binFile),
			0700,
		)
		if err != nil {
			return errutil.WrapErr(err, "error building root package command")
		}

		// Create main package.json
		pkgJson := maps.Clone(cfg.Pkg)
		pkgJson["files"] = append(pkgFiles.ToSlice(), pkgName)
		pkgJson["bin"] = binFileName
		pkgJson["preferUnplugged"] = true
		pkgJson["optionalDependencies"] = lo.SliceToMap(builds, func(build PlatformBuild) (string, string) {
			return build.pkgName, version
		})

		pkgFile, err := os.Create(
			filepath.Join(pkgDir, "package.json"),
		)
		if err != nil {
			return errutil.WrapErr(err, "error writing main package.json")
		}
		defer pkgFile.Close()

		enc := json.NewEncoder(pkgFile)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		err = enc.Encode(pkgJson)
		if err != nil {
			return errutil.WrapErr(err, "error building main package.json")
		}

		// Copy files
		for _, name := range pkgFiles.ToSlice() {
			src, err := os.Open(name)
			if err != nil {
				// Don't report errors for default files
				if name == "README.md" || name == "LICENSE" {
					continue
				}
				l.Log.Warn().Err(err).Msgf("unable to read file: %s", name)
				continue
			}
			defer src.Close()

			dst, err := os.Create(filepath.Join(pkgDir, name))
			if err != nil {
				l.Log.Err(err).Msgf("unable to write file: %s", name)
				continue
			}
			defer dst.Close()

			_, err = io.Copy(dst, src)
			if err != nil {
				l.Log.Err(err).Msgf("unable to copy file: %s", name)
				continue
			}
		}

		// Publish
		group, gCtx = errgroup.WithContext(ctx)
		if shouldPublish {
			publishCmd := "npm publish"
			if dryPublish {
				publishCmd += " --dry-run"
			}
			// Publish the first one in interactive mode
			// so user can enter token or otp code.
			build := builds[0]
			cmd := newCmd(ctx, publishCmd)
			cmd.Dir = build.pkgDir
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			err := cmd.Run()
			if err != nil {
				return err
			}

			// Publish the rest if the first completes
			if len(builds) > 1 {
				for _, build := range builds[1:] {
					group.Go(func() error {
						cmd := newCmd(ctx, publishCmd)
						var stderr bytes.Buffer
						cmd.Dir = build.pkgDir
						cmd.Stdin = os.Stdin
						cmd.Stdout = os.Stdout
						// npm publish uses stderr for output info. Buffer this.
						cmd.Stderr = &stderr
						err = cmd.Run()
						println()
						print(stderr.String())
						return err
					})
				}
			}

			// Publish the main package
			group.Go(func() error {
				cmd := newCmd(ctx, publishCmd)
				var stderr bytes.Buffer
				cmd.Dir = pkgDir
				cmd.Stdin = os.Stdin
				cmd.Stdout = os.Stdout
				cmd.Stderr = &stderr
				err = cmd.Run()
				println()
				print(stderr.String())
				return err
			})

		}

		err = group.Wait()
		if err != nil {
			return err
		}
		l.Log.Info().Msg("✨ Done")
		return nil
	},
}

type ConfigSpec struct {
	Pkg        map[string]any `json:"package.json"`
	Bin        string         `json:"bin"`
	Platforms  []string       `json:"platforms"`
	GoBuildCmd string         `json:"gobuild"`
	Files      []string       `json:"files"`
}

type PlatformBuild struct {
	pkgName  string
	binName  string
	pkgDir   string
	goos     string
	goarch   string
	nodeOs   string
	nodeArch string
}

// https://gist.github.com/asukakenji/f15ba7e588ac42795f421b48b8aede63
// See: https://nodejs.org/api/process.html#processplatform
func goOsToNode(name string) (string, error) {
	switch name {
	case "aix", "darwin", "freebsd", "linux", "openbsd":
		return name, nil
	case "solaris":
		return "sunos", nil
	case "windows":
		return "win32", nil
	}

	return "", fmt.Errorf("Platform OS `%s` is not supported on Nodejs", name)
}

// See: https://nodejs.org/api/process.html#processarch
func goArchToNode(name string) (string, error) {
	switch name {
	case "arm", "arm64", "loong64", "mips", "mipsel", "ppc64", "riscv64", "s390", "s390x":
		return name, nil
	case "amd64":
		return "x64", nil
	case "386":
		return "ia32", nil
	}

	return "", fmt.Errorf("Platform Architecture `%s` is not supported on Nodejs", name)
}

func newCmd(ctx context.Context, scripts ...string) *exec.Cmd {
	lines, _ := shlex.Split(strings.Join(scripts, " "))
	cmd := exec.CommandContext(ctx, lines[0], lines[1:]...)
	cmd.Env = os.Environ()
	return cmd
}
