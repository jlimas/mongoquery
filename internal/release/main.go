// Command release builds the assets attached to a CalVer GitHub Release.
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type target struct{ os, arch string }

var targets = []target{
	{"linux", "amd64"}, {"linux", "arm64"},
	{"darwin", "amd64"}, {"darwin", "arm64"},
	{"windows", "amd64"}, {"windows", "arm64"},
}

func validateVersion(version string) error {
	if !regexp.MustCompile(`^v[1-9][0-9]{3}\.[0-9]{2}\.[0-9]{2}\.[1-9][0-9]*$`).MatchString(version) {
		return fmt.Errorf("version must use CalVer vYYYY.MM.DD.N with N starting at 1")
	}
	date := version[1:strings.LastIndexByte(version, '.')]
	if _, err := time.Parse("2006.01.02", date); err != nil {
		return fmt.Errorf("invalid release date: %w", err)
	}
	return nil
}

func main() {
	version := flag.String("version", "", "CalVer release tag, e.g. v2026.11.17.1")
	output := flag.String("output", "dist", "new directory for release assets")
	flag.Parse()
	if err := run(*version, *output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(version, output string) error {
	if err := validateVersion(version); err != nil {
		return err
	}
	if err := os.Mkdir(output, 0755); err != nil {
		return fmt.Errorf("create fresh output directory: %w", err)
	}
	scratch, err := os.MkdirTemp(output, ".build-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	var manifest strings.Builder
	checksums := make(map[string]string)
	for _, platform := range targets {
		name := "mongoquery"
		if platform.os == "windows" {
			name += ".exe"
		}
		binary := filepath.Join(scratch, name)
		if err := buildBinary(context.Background(), ".", binary, version, platform); err != nil {
			return err
		}
		extension := ".tar.gz"
		if platform.os == "windows" {
			extension = ".zip"
		}
		// Stable asset names support GitHub's /releases/latest/download URLs.
		archive := fmt.Sprintf("mongoquery_%s_%s%s", platform.os, platform.arch, extension)
		files := []string{binary, "LICENSE", "README.md", "config.example.yaml"}
		path := filepath.Join(output, archive)
		if err := createArchive(path, files, platform.os == "windows"); err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checksum := fmt.Sprintf("%x", sha256.Sum256(data))
		checksums[platform.os+"_"+platform.arch] = checksum
		fmt.Fprintf(&manifest, "%s  %s\n", checksum, archive)
	}
	formula, err := homebrewFormula(version, checksums)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(output, "mongoquery.rb"), formula, 0644); err != nil {
		return err
	}
	fmt.Fprintf(&manifest, "%x  mongoquery.rb\n", sha256.Sum256(formula))
	return os.WriteFile(filepath.Join(output, "checksums.txt"), []byte(manifest.String()), 0644)
}

func buildBinary(ctx context.Context, root, output, version string, platform target) error {
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags", "-s -w -X main.version="+version, "-o", output, ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+platform.os, "GOARCH="+platform.arch)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("build %s/%s: %w", platform.os, platform.arch, err)
	}
	return nil
}

func createArchive(path string, files []string, windows bool) (err error) {
	output, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
	}()
	if windows {
		writer := zip.NewWriter(output)
		for i, path := range files {
			data, err := os.ReadFile(path)
			if err != nil {
				writer.Close()
				return err
			}
			header := &zip.FileHeader{Name: filepath.Base(path), Method: zip.Deflate}
			header.SetMode(0644)
			if i == 0 {
				header.SetMode(0755)
			}
			entry, err := writer.CreateHeader(header)
			if err == nil {
				_, err = entry.Write(data)
			}
			if err != nil {
				writer.Close()
				return err
			}
		}
		return writer.Close()
	}
	compressed := gzip.NewWriter(output)
	writer := tar.NewWriter(compressed)
	for i, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			writer.Close()
			compressed.Close()
			return err
		}
		mode := int64(0644)
		if i == 0 {
			mode = 0755
		}
		err = writer.WriteHeader(&tar.Header{Name: filepath.Base(path), Mode: mode, Size: int64(len(data))})
		if err == nil {
			_, err = writer.Write(data)
		}
		if err != nil {
			writer.Close()
			compressed.Close()
			return err
		}
	}
	if err := writer.Close(); err != nil {
		compressed.Close()
		return err
	}
	return compressed.Close()
}
