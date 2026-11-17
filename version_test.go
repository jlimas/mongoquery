package main

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

func TestVersionWorksWithoutConfig(t *testing.T) {
	t.Setenv("MONGOQUERY_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	for _, arg := range []string{"version", "--version"} {
		t.Run(arg, func(t *testing.T) {
			out, stderr, err := captureCommand(t, func() error { return run([]string{arg}) })
			if err != nil || stderr != "" || out != "mongoquery dev\n" {
				t.Fatalf("stdout=%q stderr=%q err=%v", out, stderr, err)
			}
		})
	}
}

func TestVersionRejectsExtraArguments(t *testing.T) {
	for _, arg := range []string{"version", "--version"} {
		out, stderr, err := captureCommand(t, func() error { return run([]string{arg, "extra"}) })
		if err == nil || out != "" || stderr == "" {
			t.Fatalf("extra argument accepted: stdout=%q stderr=%q err=%v", out, stderr, err)
		}
	}
}

func TestVersionPropagatesWriteFailure(t *testing.T) {
	if err := printVersion(failingWriter{}); err == nil {
		t.Fatal("version output failure reported success")
	}
}

func TestBuiltBinaryReportsEmbeddedVersion(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mongoquery")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-ldflags", "-X main.version=v1.2.3", "-o", binary, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	t.Setenv("MONGOQUERY_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	for _, arg := range []string{"version", "--version"} {
		command := exec.Command(binary, arg)
		out, err := command.CombinedOutput()
		if err != nil || string(out) != "mongoquery v1.2.3\n" {
			t.Fatalf("%s: output=%q err=%v", arg, out, err)
		}
	}
}
