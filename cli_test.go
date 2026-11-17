package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Tests using process streams or environment variables must not run in parallel.
func captureCommand(t *testing.T, command func() error) (stdout, stderr string, commandErr error) {
	t.Helper()
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	errOut, err := os.CreateTemp(t.TempDir(), "stderr")
	if err != nil {
		t.Fatal(err)
	}
	defer errOut.Close()
	originalOut, originalErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = out, errOut
	defer func() { os.Stdout, os.Stderr = originalOut, originalErr }()
	commandErr = command()
	read := func(path string) string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(data)
	}
	return read(out.Name()), read(errOut.Name()), commandErr
}

func TestSubcommandHelpSucceedsWithoutConfig(t *testing.T) {
	t.Setenv("MONGOQUERY_CONFIG", filepath.Join(t.TempDir(), "missing.yaml"))
	for _, command := range []string{"find", "aggregate", "count", "distinct", "collections"} {
		t.Run(command, func(t *testing.T) {
			out, stderr, err := captureCommand(t, func() error { return run([]string{command, "--help"}) })
			if err != nil || stderr != "" || !strings.Contains(out, "usage: mongoquery "+command) {
				t.Fatalf("help failed: stdout=%q stderr=%q err=%v", out, stderr, err)
			}
		})
	}
}

func TestUsageErrorsDoNotPolluteStdout(t *testing.T) {
	for _, args := range [][]string{
		{"unknown"}, {"find", "oops", "--limit", "1"}, {"list", "oops"},
		{"find", "--format", "csv"}, {"count", "--timeout", "0"},
		{"aggregate", "--env", "prod", "--service", "app", "--collection", "users"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			out, stderr, err := captureCommand(t, func() error { return run(args) })
			if err == nil || out != "" || stderr == "" {
				t.Fatalf("want error on stderr only, got stdout=%q stderr=%q err=%v", out, stderr, err)
			}
		})
	}
}

func TestFindJSONIsStandaloneAndHonorsFields(t *testing.T) {
	newMongoStub(t, false)
	out, stderr, err := captureCommand(t, func() error {
		return run([]string{"find", "--env", "test", "--service", "app", "--collection", "users", "--format", "json", "--fields", "name"})
	})
	var docs []map[string]string
	if err != nil || stderr != "" || json.Unmarshal([]byte(out), &docs) != nil {
		t.Fatalf("invalid JSON output: %q, stderr=%q, err=%v", out, stderr, err)
	}
	if len(docs) != 1 || len(docs[0]) != 1 || docs[0]["name"] != "Ada" {
		t.Fatalf("unexpected documents: %v", docs)
	}
}
