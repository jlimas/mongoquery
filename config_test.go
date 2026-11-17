package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestConfigRejectsTyposAndExtraDocuments(t *testing.T) {
	for _, input := range []string{
		"environmnts: {}\n",
		"environments:\n  prod:\n    services:\n      app:\n        uri: mongodb://localhost/db\n        databse: wrong\n",
		"environments: {}\n",
		"environments: {prod: {services: {}}}\n---\nenvironments: {}\n",
	} {
		path := filepath.Join(t.TempDir(), "config.yaml")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MONGOQUERY_CONFIG", path)
		if _, err := loadConfig(); err == nil {
			t.Fatalf("invalid configuration accepted: %s", input)
		}
	}
}

func TestConfigResolvesSelectedTargetAndDatabaseOverride(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	data := "environments:\n  prod:\n    services:\n      app:\n        uri: mongodb://localhost/from_uri\n        database: explicit\n"
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MONGOQUERY_CONFIG", path)
	cfg, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	svc, err := cfg.resolve("prod", "app")
	if err != nil || svc.Database != "explicit" || svc.URI != "mongodb://localhost/from_uri" {
		t.Fatalf("target configuration changed: %+v, %v", svc, err)
	}
	if _, err := cfg.resolve("typo", "app"); err == nil || !strings.Contains(err.Error(), "prod") {
		t.Fatalf("missing actionable target error: %v", err)
	}
}

func TestMakeConfigUsesOverrideAndPrivatePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Makefile uses a POSIX shell")
	}
	if _, err := exec.LookPath("make"); err != nil {
		t.Skip("make not available")
	}
	path := filepath.Join(t.TempDir(), "config with spaces.yaml")
	t.Setenv("MONGOQUERY_CONFIG", path)
	runMake := func() {
		t.Helper()
		if output, err := exec.Command("make", "config").CombinedOutput(); err != nil {
			t.Fatalf("make config: %v\n%s", err, output)
		}
	}
	runMake()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("credentials would be exposed: mode %o", info.Mode().Perm())
	}
	if err := os.WriteFile(path, []byte("keep existing credentials"), 0600); err != nil {
		t.Fatal(err)
	}
	runMake()
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "keep existing credentials" {
		t.Fatalf("existing config overwritten: %q, %v", data, err)
	}
}
