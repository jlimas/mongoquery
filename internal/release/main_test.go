package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCalVer(t *testing.T) {
	for _, version := range []string{"v2026.11.17.1", "v2026.01.01.12", "v2024.02.29.1"} {
		if err := validateVersion(version); err != nil {
			t.Errorf("%s rejected: %v", version, err)
		}
	}
	for _, version := range []string{
		"", "v1.2.3", "2026.11.17.1", "v2026.1.01.1", "v2026.01.1.1",
		"v2026.02.29.1", "v2026.04.31.1", "v2026.13.01.1", "v2026.01.01.0",
		"v2026.01.01.01", "v2026.01.01.-1", "v2026.01.01.1-rc1", "v2026.01.01.1\n",
	} {
		if err := validateVersion(version); err == nil {
			t.Errorf("invalid tag accepted: %q", version)
		}
	}
}

func TestInvalidVersionDoesNotCreateAssets(t *testing.T) {
	output := filepath.Join(t.TempDir(), "assets")
	if err := run("v2026.02.30.1", output); err == nil {
		t.Fatal("invalid release succeeded")
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("invalid release created output: %v", err)
	}
}

func TestReleaseDoesNotOverwriteExistingOutput(t *testing.T) {
	output := t.TempDir()
	sentinel := filepath.Join(output, "keep")
	if err := os.WriteFile(sentinel, []byte("existing release"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := run("v2026.11.17.1", output); err == nil {
		t.Fatal("existing output directory accepted")
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "existing release" {
		t.Fatalf("existing assets changed: %q, %v", data, err)
	}
}

func TestReleaseBinaryEmbedsCalVer(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	name := "mongoquery"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(t.TempDir(), name)
	if err := buildBinary(context.Background(), root, binary, "v2026.11.17.2", target{runtime.GOOS, runtime.GOARCH}); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(binary, "--version")
	command.Env = append(os.Environ(), "MONGOQUERY_CONFIG="+filepath.Join(t.TempDir(), "missing"))
	data, err := command.CombinedOutput()
	if err != nil || string(data) != "mongoquery v2026.11.17.2\n" {
		t.Fatalf("wrong release binary version: %q, %v", data, err)
	}
}

func TestArchivesPreserveFilesAndExecutableMode(t *testing.T) {
	for _, windows := range []bool{false, true} {
		t.Run(fmt.Sprint("windows=", windows), func(t *testing.T) {
			directory := t.TempDir()
			files := []string{filepath.Join(directory, "mongoquery"), filepath.Join(directory, "LICENSE")}
			for _, path := range files {
				if err := os.WriteFile(path, []byte("contents of "+filepath.Base(path)), 0600); err != nil {
					t.Fatal(err)
				}
			}
			archive := filepath.Join(directory, "archive")
			if err := createArchive(archive, files, windows); err != nil {
				t.Fatal(err)
			}
			got := map[string]string{}
			var binaryMode os.FileMode
			if windows {
				reader, err := zip.OpenReader(archive)
				if err != nil {
					t.Fatal(err)
				}
				defer reader.Close()
				for _, entry := range reader.File {
					file, err := entry.Open()
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(file)
					file.Close()
					if err != nil {
						t.Fatal(err)
					}
					got[entry.Name] = string(data)
					if entry.Name == "mongoquery" {
						binaryMode = entry.Mode()
					}
				}
			} else {
				file, err := os.Open(archive)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				compressed, err := gzip.NewReader(file)
				if err != nil {
					t.Fatal(err)
				}
				defer compressed.Close()
				reader := tar.NewReader(compressed)
				for {
					header, err := reader.Next()
					if err == io.EOF {
						break
					}
					if err != nil {
						t.Fatal(err)
					}
					data, err := io.ReadAll(reader)
					if err != nil {
						t.Fatal(err)
					}
					got[header.Name] = string(data)
					if header.Name == "mongoquery" {
						binaryMode = os.FileMode(header.Mode)
					}
				}
			}
			if len(got) != 2 || got["mongoquery"] != "contents of mongoquery" || got["LICENSE"] != "contents of LICENSE" {
				t.Fatalf("archive changed files or leaked paths: %v", got)
			}
			if binaryMode.Perm() != 0755 {
				t.Fatalf("binary not executable: %o", binaryMode)
			}
		})
	}
}

func TestHomebrewFormulaUsesMatchingPlatformChecksums(t *testing.T) {
	checksums := map[string]string{}
	for i, platform := range targets[:4] {
		// Distinct hashes make swapped architecture/OS checksums detectable.
		checksums[platform.os+"_"+platform.arch] = fmt.Sprintf("%064x", i+1)
	}
	formula, err := homebrewFormula("v2026.11.17.2", checksums)
	if err != nil {
		t.Fatal(err)
	}
	for platform, hash := range checksums {
		expected := fmt.Sprintf("/v2026.11.17.2/mongoquery_%s.tar.gz\"\n      sha256 \"%s\"", platform, hash)
		if !strings.Contains(string(formula), expected) {
			t.Errorf("incorrect URL/hash pairing for %s:\n%s", platform, formula)
		}
	}
	if !strings.Contains(string(formula), `version "2026.11.17.2"`) {
		t.Fatalf("missing formula version:\n%s", formula)
	}
	delete(checksums, "linux_arm64")
	if _, err := homebrewFormula("v2026.11.17.2", checksums); err == nil {
		t.Fatal("formula accepted missing platform checksum")
	}
}

func TestInstallerVerifiesArchiveBeforeInstalling(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX installer")
	}
	for _, tt := range []struct {
		system, machine, platform string
		tamper                    bool
	}{
		{"Linux", "x86_64", "linux_amd64", false},
		{"Linux", "aarch64", "linux_arm64", false},
		{"Darwin", "x86_64", "darwin_amd64", false},
		{"Darwin", "arm64", "darwin_arm64", false},
		{"Linux", "x86_64", "linux_amd64", true},
	} {
		t.Run(fmt.Sprintf("%s/tamper=%t", tt.platform, tt.tamper), func(t *testing.T) {
			directory := t.TempDir()
			fixtures := filepath.Join(directory, "downloads")
			bin := filepath.Join(directory, "tools")
			destination := filepath.Join(directory, "install with spaces")
			for _, path := range []string{fixtures, bin, destination} {
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
			}
			write := func(path, data string, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, []byte(data), mode); err != nil {
					t.Fatal(err)
				}
			}
			payload := filepath.Join(directory, "mongoquery")
			write(payload, "#!/bin/sh\necho 'mongoquery v2026.11.17.1'\n", 0755)
			archiveName := "mongoquery_" + tt.platform + ".tar.gz"
			archive := filepath.Join(fixtures, archiveName)
			if err := createArchive(archive, []string{payload}, false); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(archive)
			if err != nil {
				t.Fatal(err)
			}
			hash := fmt.Sprintf("%x", sha256.Sum256(data))
			if tt.tamper {
				hash = strings.Repeat("0", 64)
			}
			write(filepath.Join(fixtures, "checksums.txt"), hash+"  "+archiveName+"\n", 0644)
			installed := filepath.Join(destination, "mongoquery")
			write(installed, "keep existing install", 0755)
			// Stub only platform/network discovery; real tar, SHA-256, and install
			// utilities exercise the integrity and installation path.
			write(filepath.Join(bin, "uname"), "#!/bin/sh\ncase \"$1\" in -s) echo \"$TEST_SYSTEM\";; -m) echo \"$TEST_MACHINE\";; esac\n", 0755)
			write(filepath.Join(bin, "curl"), `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    -o) output=$2; shift 2;;
    --retry) shift 2;;
    -*) shift;;
    *) url=$1; shift;;
  esac
done
case "$url" in
  https://github.com/jlimas/mongoquery/releases/latest/download/*)
    cp "$TEST_DOWNLOADS/${url##*/}" "$output";;
  *) exit 1;;
esac
`, 0755)
			command := exec.Command("sh", "../../scripts/install.sh")
			command.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"MONGOQUERY_INSTALL_DIR="+destination, "TEST_DOWNLOADS="+fixtures,
				"TEST_SYSTEM="+tt.system, "TEST_MACHINE="+tt.machine)
			out, err := command.CombinedOutput()
			if (err != nil) != tt.tamper {
				t.Fatalf("unexpected install outcome: %v\n%s", err, out)
			}
			got, err := os.ReadFile(installed)
			if err != nil {
				t.Fatal(err)
			}
			if tt.tamper {
				if string(got) != "keep existing install" {
					t.Fatal("checksum failure replaced existing installation")
				}
			} else {
				expected, err := os.ReadFile(payload)
				if err != nil || string(got) != string(expected) {
					t.Fatalf("wrong installed binary: %q, %v", got, err)
				}
				info, err := os.Stat(installed)
				if err != nil || info.Mode().Perm() != 0755 {
					t.Fatalf("installed binary not executable: %v, %v", info, err)
				}
			}
		})
	}
}
