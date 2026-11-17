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
	if _, err := exec.LookPath("goreleaser"); err != nil {
		return fmt.Errorf("goreleaser not found (run mise install): %w", err)
	}
	out := "pear"
	if runtime.GOOS == "windows" {
		out = "pear.exe"
	}
	cmd := exec.Command(
		"goreleaser", "build",
		"--single-target",
		"--snapshot",
		"--clean",
		"--output", out,
	)
	cmd.Env = append(os.Environ(), "PEAR_VERSION="+describeVersion())
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("goreleaser build: %w", err)
	}
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
