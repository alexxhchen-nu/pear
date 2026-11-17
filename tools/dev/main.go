// Command dev is the portable half of the mise tasks. It stays free of
// shell syntax so the same tasks work under sh and cmd.exe.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func main() {
	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "build":
		err = build()
	case "fmtcheck":
		err = fmtcheck()
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: go run ./tools/dev <build|fmtcheck>")
}

// normalizeVersion turns `git describe` output into the form GoReleaser
// stores in .Version: no leading v, no whitespace. Anything unusable
// becomes the plain-build default, "dev".
func normalizeVersion(raw string) string {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")
	if v == "" || strings.ContainsAny(v, " \t\n\r\"'`$") {
		return "dev"
	}
	return v
}

func describeVersion() string {
	out, err := exec.Command("git", "describe", "--tags", "--always", "--dirty").Output()
	if err != nil {
		return "dev"
	}
	return normalizeVersion(string(out))
}

func build() error {
	version := describeVersion()
	out := binaryName()
	// GoReleaser v2 returns ErrNoGit when the git binary is missing, even
	// with --snapshot. A source archive still needs a static binary; the
	// flags here are the same ones .goreleaser.yaml passes to the release.
	if _, err := exec.LookPath("git"); err != nil {
		fmt.Fprintln(os.Stderr, "git not found; building with go (version dev)")
		return goBuild(out, "dev")
	}
	if _, err := exec.LookPath("goreleaser"); err != nil {
		return fmt.Errorf("goreleaser not found (run mise install): %w", err)
	}
	cmd := exec.Command(
		"goreleaser", "build",
		"--single-target",
		"--snapshot",
		"--clean",
		"--output", out,
	)
	cmd.Env = append(os.Environ(), "PEAR_VERSION="+version)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("goreleaser build: %w", err)
	}
	return runVersion(out)
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "pear.exe"
	}
	return "pear"
}

func goBuild(out, version string) error {
	ldflags := "-s -w -X main.version=" + version
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", out, "./cmd/pear")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("go build: %w", err)
	}
	return runVersion(out)
}

func runVersion(out string) error {
	// Abs keeps a path separator. filepath.Join(".", "pear") cleans to
	// "pear", and exec would then search PATH instead of the file just built.
	bin, err := filepath.Abs(out)
	if err != nil {
		return err
	}
	ver := exec.Command(bin, "version")
	ver.Stdout = os.Stdout
	ver.Stderr = os.Stderr
	if err := ver.Run(); err != nil {
		return fmt.Errorf("%s version: %w", out, err)
	}
	return nil
}

func fmtcheck() error {
	cmd := exec.Command("gofmt", "-l", ".")
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			fmt.Fprint(os.Stderr, string(ee.Stderr))
		}
		return fmt.Errorf("gofmt: %w", err)
	}
	files := strings.TrimSpace(string(out))
	if files != "" {
		return fmt.Errorf("gofmt would rewrite:\n%s", files)
	}
	return nil
}
