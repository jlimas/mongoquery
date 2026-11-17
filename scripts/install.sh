#!/bin/sh
set -eu

fail() { echo "mongoquery: $*" >&2; exit 1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) fail "unsupported OS; download a Windows zip from GitHub Releases" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) fail "unsupported architecture: $(uname -m)" ;;
esac

command -v curl >/dev/null 2>&1 || fail "curl is required"
command -v tar >/dev/null 2>&1 || fail "tar is required"
if command -v sha256sum >/dev/null 2>&1; then
  checksum_tool=sha256sum
elif command -v shasum >/dev/null 2>&1; then
  checksum_tool=shasum
else
  fail "sha256sum or shasum is required"
fi

destination=${MONGOQUERY_INSTALL_DIR:-"$HOME/.local/bin"}
base=https://github.com/jlimas/mongoquery/releases/latest/download
archive="mongoquery_${os}_${arch}.tar.gz"
temp=$(mktemp -d)
trap 'rm -rf "$temp"' EXIT
trap 'exit 1' HUP INT TERM

curl -fsSL --retry 3 -o "$temp/$archive" "$base/$archive"
curl -fsSL --retry 3 -o "$temp/checksums.txt" "$base/checksums.txt"
expected=$(awk -v name="$archive" '$2 == name { n++; sum=$1 } END { if (n != 1) exit 1; print sum }' "$temp/checksums.txt") ||
  fail "archive checksum is missing or ambiguous"
if [ "$checksum_tool" = sha256sum ]; then
  actual=$(sha256sum "$temp/$archive")
else
  actual=$(shasum -a 256 "$temp/$archive")
fi
actual=${actual%% *}
[ "$actual" = "$expected" ] || fail "checksum verification failed; nothing installed"

tar -xzf "$temp/$archive" -C "$temp" mongoquery
mkdir -p "$destination"
install -m 0755 "$temp/mongoquery" "$destination/mongoquery"
echo "Installed $destination/mongoquery"
case ":$PATH:" in
  *":$destination:"*) ;;
  *) echo "Add $destination to your PATH to run mongoquery." ;;
esac
