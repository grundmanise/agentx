#!/bin/sh
# Install the agentx CLI from its GitHub releases.
#
#   curl -fsSL https://agentx.wtf/install | sh                       # the latest release
#   curl -fsSL https://agentx.wtf/install | sh -s -- --nightly       # the newest nightly
#   curl -fsSL https://agentx.wtf/install | sh -s -- --version 0.2.0
#
# Options:
#   --nightly        install the newest nightly build of main
#   --version X      install version X
#   --dir DIR        install into DIR, by default $AGENTX_INSTALL_DIR or ~/.local/bin
#
# Run it again to update. Set GITHUB_TOKEN to lift GitHub's limit of 60 API requests an
# hour, which only --nightly uses.
#
# Everything runs from main, called on the last line, so a download cut short runs nothing.

set -eu

repo=grundmanise/agentx

main() {
	channel=stable
	version=
	dir=${AGENTX_INSTALL_DIR:-${HOME:?}/.local/bin}
	while [ $# -gt 0 ]; do
		case $1 in
		--nightly) channel=nightly ;;
		--version)
			[ $# -ge 2 ] || die "--version needs a version, such as 0.2.0"
			version=$2
			shift
			;;
		--version=*) version=${1#*=} ;;
		--dir)
			[ $# -ge 2 ] || die "--dir needs a directory"
			dir=$2
			shift
			;;
		--dir=*) dir=${1#*=} ;;
		-h | --help)
			usage
			exit 0
			;;
		*) die "unknown option $1, see --help" ;;
		esac
		shift
	done
	[ -z "$version" ] || [ "$channel" = stable ] || die "use --nightly or --version, not both"

	need curl
	need tar
	platform=$(detect_platform)
	asset=agentx_$platform.tar.gz
	if [ -n "$version" ]; then
		tag=v${version#v}
	elif [ "$channel" = nightly ]; then
		tag=$(newest_nightly)
	else
		tag=$(latest_release)
	fi

	tmp=$(mktemp -d)
	trap 'rm -rf "$tmp"' EXIT
	trap 'exit 1' INT TERM
	say "downloading agentx ${tag#v} for $platform"
	fetch "https://github.com/$repo/releases/download/$tag/$asset" "$tmp/$asset" ||
		die "release $tag has no $asset, see https://github.com/$repo/releases"
	fetch "https://github.com/$repo/releases/download/$tag/checksums.txt" "$tmp/checksums.txt" ||
		die "release $tag has no checksums.txt"
	verify "$tmp" "$asset"
	tar -xzf "$tmp/$asset" -C "$tmp" agentx

	# A rename replaces the file in one step, even while agentx runs.
	mkdir -p "$dir"
	cp "$tmp/agentx" "$dir/.agentx.$$"
	chmod 755 "$dir/.agentx.$$"
	mv -f "$dir/.agentx.$$" "$dir/agentx"
	say "installed agentx ${tag#v} to $dir/agentx"

	case :${PATH:-}: in
	*:"$dir":*)
		found=$(command -v agentx || true)
		[ "$found" = "$dir/agentx" ] ||
			say "warning: $found comes first on your PATH, so agentx runs that copy; remove it or move $dir ahead of it"
		;;
	*) say "add $dir to your PATH to run agentx, for example in your shell's startup file: export PATH=\"$dir:\$PATH\"" ;;
	esac
}

usage() {
	cat <<EOF
Install the agentx CLI from https://github.com/$repo/releases.

Usage: install.sh [--nightly | --version X] [--dir DIR]

  --nightly      install the newest nightly build of main
  --version X    install version X, such as 0.2.0
  --dir DIR      install into DIR (default: \$AGENTX_INSTALL_DIR or ~/.local/bin)
EOF
}

say() { printf 'agentx install: %s\n' "$1" >&2; }
die() {
	say "error: $1"
	exit 1
}
need() { command -v "$1" > /dev/null 2>&1 || die "needs $1, install it and try again"; }
fetch() { curl -fsSL --proto '=https' --tlsv1.2 -o "$2" "$1"; }

detect_platform() {
	case $(uname -s) in
	Linux) os=linux ;;
	Darwin) os=darwin ;;
	*) die "agentx runs on Linux and macOS, not $(uname -s)" ;;
	esac
	case $(uname -m) in
	x86_64 | amd64) arch=amd64 ;;
	arm64 | aarch64) arch=arm64 ;;
	*) die "agentx has no build for $(uname -m)" ;;
	esac
	# A shell under Rosetta reports x86_64 on Apple silicon: install the native build.
	if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2> /dev/null)" = 1 ]; then
		arch=arm64
	fi
	echo "${os}_$arch"
}

# GitHub redirects /releases/latest to the newest release that is not a prerelease.
latest_release() {
	url=$(curl -fsSLI --proto '=https' --tlsv1.2 -o /dev/null -w '%{url_effective}' "https://github.com/$repo/releases/latest") ||
		die "cannot reach github.com"
	case $url in
	*/releases/tag/*) echo "${url##*/}" ;;
	*) die "agentx has no release yet, try --nightly" ;;
	esac
}

# Releases come newest first; nightlies are the prereleases tagged vX.Y.Z-nightly.YYYYMMDD.
newest_nightly() {
	api="https://api.github.com/repos/$repo/releases?per_page=50"
	if [ -n "${GITHUB_TOKEN:-}" ]; then
		json=$(curl -fsSL --proto '=https' --tlsv1.2 -H "Authorization: Bearer $GITHUB_TOKEN" "$api")
	else
		json=$(curl -fsSL --proto '=https' --tlsv1.2 "$api")
	fi || die "cannot list the releases; GitHub allows 60 requests an hour without GITHUB_TOKEN"
	tag=$(printf '%s\n' "$json" | grep -o '"tag_name": *"v[^"]*-nightly\.[^"]*"' | sed -n 's/.*"\(v[^"]*\)"$/\1/p' | sed -n 1p)
	[ -n "$tag" ] || die "there is no nightly release yet"
	echo "$tag"
}

verify() {
	expected=$(awk -v f="$2" '$2 == f { print $1 }' "$1/checksums.txt")
	[ -n "$expected" ] || die "checksums.txt lists no $2"
	if command -v sha256sum > /dev/null 2>&1; then
		actual=$(sha256sum "$1/$2" | cut -d' ' -f1)
	elif command -v shasum > /dev/null 2>&1; then
		actual=$(shasum -a 256 "$1/$2" | cut -d' ' -f1)
	else
		die "needs sha256sum or shasum to check the download"
	fi
	[ "$actual" = "$expected" ] || die "$2 does not match its checksum, try again"
}

main "$@"
