package main

import (
	"fmt"
	"regexp"
	"strings"
)

func homebrewFormula(version string, checksums map[string]string) ([]byte, error) {
	if err := validateVersion(version); err != nil {
		return nil, err
	}
	var formula strings.Builder
	fmt.Fprintf(&formula, `class Mongoquery < Formula
  desc "Read-only MongoDB query CLI"
  homepage "https://github.com/jlimas/mongoquery"
  version "%s"
  license "MIT"

`, strings.TrimPrefix(version, "v"))
	for _, os := range []string{"darwin", "linux"} {
		platform := "macos"
		if os == "linux" {
			platform = "linux"
		}
		fmt.Fprintf(&formula, "  on_%s do\n", platform)
		for _, arch := range []string{"arm64", "amd64"} {
			checksum := checksums[os+"_"+arch]
			if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(checksum) {
				return nil, fmt.Errorf("missing or invalid checksum for %s/%s", os, arch)
			}
			cpu := "arm"
			if arch == "amd64" {
				cpu = "intel"
			}
			fmt.Fprintf(&formula, `    on_%s do
      url "https://github.com/jlimas/mongoquery/releases/download/%s/mongoquery_%s_%s.tar.gz"
      sha256 "%s"
    end
`, cpu, version, os, arch, checksum)
		}
		formula.WriteString("  end\n\n")
	}
	formula.WriteString(`  def install
    bin.install "mongoquery"
    pkgshare.install "config.example.yaml"
  end

  test do
    assert_equal "mongoquery v#{version}", shell_output("#{bin}/mongoquery --version").strip
  end
end
`)
	return []byte(formula.String()), nil
}
