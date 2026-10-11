#!/bin/sh
# Installs dr, a terminal tool to review code that AI agents wrote.
# https://github.com/JinmuGo/dr
#
#   curl -fsSL https://dr.jinmugo.com/install.sh | sh
#   curl -fsSL https://dr.jinmugo.com/install.sh | sh -s -- --version 0.1.0 --dir /usr/local/bin
#
# It downloads the release archive for this system from GitHub, checks it against the
# release's checksums.txt, and puts the dr binary in ~/.local/bin or the directory given.
# DR_VERSION and DR_INSTALL_DIR do the same as --version and --dir. DR_DOWNLOAD_URL
# points at a mirror that holds the same files as a GitHub release.

set -eu

repo="JinmuGo/dr"
version="${DR_VERSION:-latest}"
dir="${DR_INSTALL_DIR:-$HOME/.local/bin}"
base="${DR_DOWNLOAD_URL:-}"

say() {
	printf '%s\n' "$*" >&2
}

die() {
	printf 'dr install: %s\n' "$*" >&2
	exit 1
}

usage() {
	cat >&2 <<'EOF'
Install dr from its GitHub release.

usage: install.sh [--version <version>] [--dir <directory>]

  --version  release to install, such as 0.1.0 (default: the latest release)
  --dir      directory for the dr binary (default: ~/.local/bin)
EOF
}

while [ $# -gt 0 ]; do
	case "$1" in
	--version)
		[ $# -ge 2 ] || die "--version needs a value"
		version="$2"
		shift 2
		;;
	--version=*)
		version="${1#*=}"
		shift
		;;
	--dir)
		[ $# -ge 2 ] || die "--dir needs a value"
		dir="$2"
		shift 2
		;;
	--dir=*)
		dir="${1#*=}"
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage
		die "unknown option: $1"
		;;
	esac
done

os=$(uname -s)
case "$os" in
Darwin | Linux) ;;
*) die "dr runs on macOS and Linux, not $os" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=x86_64 ;;
arm64 | aarch64) arch=arm64 ;;
*) die "there is no dr release for $arch" ;;
esac
# A shell running under Rosetta reports x86_64 on an Apple silicon Mac.
if [ "$os" = Darwin ] && [ "$arch" = x86_64 ] && [ "$(sysctl -n hw.optional.arm64 2>/dev/null || true)" = 1 ]; then
	arch=arm64
fi

case "$version" in
latest | v*) ;;
*) version="v$version" ;;
esac
if [ -z "$base" ]; then
	if [ "$version" = latest ]; then
		base="https://github.com/$repo/releases/latest/download"
	else
		base="https://github.com/$repo/releases/download/$version"
	fi
fi

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -O "$2" "$1"
	else
		die "curl or wget is needed to download dr"
	fi
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		die "sha256sum or shasum is needed to check the download"
	fi
}

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t dr)
trap 'rm -rf "$tmp"' EXIT
trap 'exit 1' HUP INT TERM

archive="dr_${os}_${arch}.tar.gz"
say "Downloading $archive ($version)"
fetch "$base/$archive" "$tmp/$archive" || die "could not download $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download $base/checksums.txt"

want=$(awk -v name="$archive" '$2 == name { print $1 }' "$tmp/checksums.txt")
[ -n "$want" ] || die "checksums.txt has no entry for $archive"
got=$(sha256 "$tmp/$archive")
[ "$want" = "$got" ] || die "the checksum of $archive does not match: want $want, got $got"

tar -xzf "$tmp/$archive" -C "$tmp" dr || die "could not unpack $archive"
mkdir -p "$dir" || die "could not create $dir"
# Copy next to the target and rename, so a dr that is running is never half written.
part="$dir/.dr.$$"
if ! cp "$tmp/dr" "$part" 2>/dev/null; then
	die "could not write to $dir; pick another directory with --dir, or run with sudo"
fi
chmod 0755 "$part"
mv -f "$part" "$dir/dr"

installed=$("$dir/dr" --version 2>/dev/null || echo dr)
say "Installed $installed to $dir/dr"
case ":$PATH:" in
*":$dir:"*) ;;
*) say "$dir is not on your PATH. Add it with: export PATH=\"$dir:\$PATH\"" ;;
esac
say "dr needs git. To ask and fix, it runs the CLI of the agent that wrote the lines: claude, codex or omp."
