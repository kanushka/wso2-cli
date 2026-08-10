#!/usr/bin/env bash
# Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
#
# WSO2 LLC. licenses this file to you under the Apache License,
# Version 2.0 (the "License"); you may not use this file except
# in compliance with the License.
# You may obtain a copy of the License at
#
# http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing,
# software distributed under the License is distributed on an
# "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
# KIND, either express or implied.  See the License for the
# specific language governing permissions and limitations
# under the License.

# Installs the wso2 shell on macOS, Linux, and WSL.
#
#	curl -fsSL <install url> | bash            # newest stable release
#	curl -fsSL <install url> | bash -s v0.1.0  # a pinned release
#
# This script is meant to be read before it is run, so it stays flat and boring:
# one function per step, no indirection, and nothing that needs elevated
# privileges. It downloads an archive from a published release, verifies it
# against the checksum file published beside it, and refuses to install anything
# that fails that check.
#
# The artifact names, the checksum file, and the tag resolution it depends on are
# documented in docs/reference/release-artifacts.md.
#
# Variables it reads:
#
#	WSO2_HOME                  State root to install into. Default ~/.wso2.
#	WSO2_CLI_PRERELEASE=true   Resolve the newest prerelease, not the newest
#	                           stable release.
#	WSO2_CLI_NO_PROFILE=1      Install without editing any shell profile.
#	WSO2_CLI_RELEASE_BASE_URL  Where releases are downloaded from. Overridden by
#	WSO2_CLI_RELEASE_API_URL   the tests; users have no reason to set either.

set -euo pipefail

RELEASE_BASE_URL="${WSO2_CLI_RELEASE_BASE_URL:-https://github.com/wso2/wso2-cli/releases}"
RELEASE_API_URL="${WSO2_CLI_RELEASE_API_URL:-https://api.github.com/repos/wso2/wso2-cli/releases}"

BLOCK_BEGIN='# >>> wso2 cli >>>'
BLOCK_END='# <<< wso2 cli <<<'

# The download directory is global rather than local to main, because the EXIT
# trap that removes it runs after main's locals have gone out of scope: a local
# would leave the trap with an unset name and the directory on disk.
TEMP_DIR=''

fail() {
	printf 'error: %s\n' "$1" >&2
	exit 1
}

# require_tools fails before anything is downloaded when a tool this script
# cannot work without is missing, naming the tool rather than failing later with
# whatever error the missing command happens to produce.
require_tools() {
	local tool
	for tool in curl mktemp uname grep; do
		command -v "$tool" >/dev/null 2>&1 ||
			fail "this installer needs ${tool}, which is not on PATH."
	done
}

detect_os() {
	local os
	os="$(uname -s | tr '[:upper:]' '[:lower:]')"
	case "$os" in
	linux | darwin) printf '%s\n' "$os" ;;
	*) fail "unsupported operating system: ${os}. This installer supports macOS, Linux, and WSL." ;;
	esac
}

# detect_arch maps what the machine calls itself onto the architecture names the
# release artifacts use. An unrecognised machine is a refusal: guessing would
# download an archive built for another processor.
detect_arch() {
	local machine
	machine="$(uname -m | tr '[:upper:]' '[:lower:]')"
	case "$machine" in
	x86_64 | amd64) printf 'amd64\n' ;;
	aarch64 | arm64) printf 'arm64\n' ;;
	armv6l | armv7l | armv8l | arm) printf 'arm\n' ;;
	i386 | i486 | i586 | i686 | x86) printf '386\n' ;;
	*) fail "unsupported architecture: ${machine}. Supported: x86_64, arm64, arm, i386." ;;
	esac
}

archive_extension() {
	case "$1" in
	darwin) printf 'zip\n' ;;
	*) printf 'tar.gz\n' ;;
	esac
}

# resolve_version reports the release tag to install.
#
# An explicit argument wins. Otherwise the newest stable tag comes from the
# redirect on the release page's /latest, which needs no API token and is not
# rate limited; the prerelease opt-in has to read the release listing instead,
# because /latest deliberately skips prereleases.
resolve_version() {
	local requested="${1:-}"
	if [ -n "$requested" ]; then
		printf '%s\n' "$requested"
		return
	fi

	local tag
	if [ "${WSO2_CLI_PRERELEASE:-}" = "true" ]; then
		# The listing is newest first, and each release is read as the text between
		# one "tag_name" key and the next: the tag is the first quoted value in that
		# span and the release's own "prerelease" flag falls inside it. Splitting
		# this way rather than on braces is what makes it survive the nested objects
		# a real release carries.
		tag="$(curl -fsSL "$RELEASE_API_URL" | awk '
			BEGIN { RS = "\"tag_name\""; found = 0 }
			NR > 1 && !found && $0 ~ /"prerelease"[[:space:]]*:[[:space:]]*true/ {
				if (match($0, /"[^"]+"/)) {
					print substr($0, RSTART + 1, RLENGTH - 2)
					found = 1
				}
			}')"
		[ -n "$tag" ] || fail "no prerelease was found at ${RELEASE_API_URL}."
	else
		# The effective URL after the redirect ends in the tag.
		local resolved
		resolved="$(curl -fsSL -o /dev/null -w '%{url_effective}' "${RELEASE_BASE_URL}/latest")"
		tag="${resolved##*/}"
		[ -n "$tag" ] && [ "$tag" != "latest" ] ||
			fail "could not determine the latest release from ${RELEASE_BASE_URL}/latest."
	fi
	printf '%s\n' "$tag"
}

# verify_checksum refuses anything whose SHA-256 does not match the checksum
# published beside it. It runs before extraction, so a substituted or truncated
# download never becomes a file on disk, let alone an executable one.
#
# A machine with neither checksum tool is a refusal rather than a skipped check:
# installing an unverified executable is the outcome this exists to prevent.
verify_checksum() {
	local directory="$1" archive="$2"
	local expected actual

	expected="$(grep -F " ${archive}" "${directory}/checksums.txt" | awk '{print $1}' | head -n 1)"
	[ -n "$expected" ] ||
		fail "checksums.txt does not list ${archive}, so the download cannot be verified."

	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "${directory}/${archive}" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "${directory}/${archive}" | awk '{print $1}')"
	else
		fail 'this installer needs sha256sum or shasum to verify the download, and found neither.'
	fi

	if [ "$expected" != "$actual" ]; then
		printf 'error: checksum mismatch for %s\n  expected %s\n  actual   %s\n' \
			"$archive" "$expected" "$actual" >&2
		fail 'refusing to install an archive that failed verification.'
	fi
	printf 'Checksum verified.\n'
}

extract() {
	local directory="$1" archive="$2" into="$3"
	case "$archive" in
	*.tar.gz)
		command -v tar >/dev/null 2>&1 ||
			fail 'this installer needs tar to extract the download, which is not on PATH.'
		tar -xzf "${directory}/${archive}" -C "$into"
		;;
	*.zip)
		command -v unzip >/dev/null 2>&1 ||
			fail 'this installer needs unzip to extract the download, which is not on PATH.'
		unzip -q "${directory}/${archive}" -d "$into"
		;;
	*) fail "unrecognised archive format: ${archive}." ;;
	esac
}

# detect_profile reports the shell profile to wire PATH in, or nothing when it
# cannot tell. It prefers the interactive rc file for the running shell, because
# that is the file a user's own shell reads.
detect_profile() {
	local shell_name="${SHELL##*/}"
	local candidate

	case "$shell_name" in
	zsh) for candidate in "$HOME/.zshrc" "$HOME/.zprofile"; do
		[ -f "$candidate" ] && printf '%s\n' "$candidate" && return
	done ;;
	bash) for candidate in "$HOME/.bashrc" "$HOME/.bash_profile"; do
		[ -f "$candidate" ] && printf '%s\n' "$candidate" && return
	done ;;
	esac

	for candidate in "$HOME/.profile" "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshrc"; do
		[ -f "$candidate" ] && printf '%s\n' "$candidate" && return
	done
}

# wire_path puts the binary directory on PATH for future shells.
#
# The block is delimited and greppable so that a second run can recognise its own
# work rather than appending a duplicate, and so that removing it later is an
# exact operation rather than a judgement call about which lines were ours.
wire_path() {
	local bin_dir="$1" profile="$2"

	if grep -qF "$BLOCK_BEGIN" "$profile" 2>/dev/null; then
		printf 'PATH is already wired in %s.\n' "$profile"
		return
	fi

	{
		printf '\n%s\n' "$BLOCK_BEGIN"
		printf 'export PATH="%s:$PATH"\n' "$bin_dir"
		printf '%s\n' "$BLOCK_END"
	} >>"$profile"
	printf 'Added %s to PATH in %s.\n' "$bin_dir" "$profile"
}

print_manual_path_instructions() {
	local bin_dir="$1" reason="$2"
	printf '\n%s\n' "$reason"
	printf 'Add this line to your shell profile to run wso2 by name:\n\n'
	printf '    export PATH="%s:$PATH"\n' "$bin_dir"
}

main() {
	require_tools

	local os arch extension version state_root bin_dir archive url
	os="$(detect_os)"
	arch="$(detect_arch)"
	extension="$(archive_extension "$os")"
	version="$(resolve_version "${1:-}")"

	state_root="${WSO2_HOME:-$HOME/.wso2}"
	bin_dir="${state_root}/bin"
	archive="wso2-cli-${version}-${os}-${arch}.${extension}"
	url="${RELEASE_BASE_URL}/download/${version}/${archive}"

	printf 'Installing the WSO2 CLI %s for %s/%s.\n' "$version" "$os" "$arch"

	# Everything downloaded lands in a temporary directory that is removed however
	# this script exits, so a failed verification leaves nothing behind to run.
	TEMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t wso2-install)"
	trap 'rm -rf "${TEMP_DIR:-}"' EXIT INT TERM

	printf 'Downloading %s\n' "$url"
	curl -fSL --progress-bar -o "${TEMP_DIR}/${archive}" "$url" ||
		fail "could not download ${url}. Check that ${version} is a published release."
	curl -fsSL -o "${TEMP_DIR}/checksums.txt" "${RELEASE_BASE_URL}/download/${version}/checksums.txt" ||
		fail "could not download the checksum file for ${version}, so the archive cannot be verified."

	verify_checksum "$TEMP_DIR" "$archive"

	mkdir -p "${TEMP_DIR}/unpacked"
	extract "$TEMP_DIR" "$archive" "${TEMP_DIR}/unpacked"
	[ -f "${TEMP_DIR}/unpacked/wso2" ] ||
		fail "the archive did not contain the expected wso2 binary."

	mkdir -p "$bin_dir"
	# Installing over a running binary fails on some systems, and a partially
	# written one would be worse, so the new binary is put in place atomically.
	mv "${TEMP_DIR}/unpacked/wso2" "${bin_dir}/wso2.new"
	chmod +x "${bin_dir}/wso2.new"
	mv "${bin_dir}/wso2.new" "${bin_dir}/wso2"
	printf 'Installed %s\n' "${bin_dir}/wso2"

	if [ -n "${WSO2_CLI_NO_PROFILE:-}" ]; then
		print_manual_path_instructions "$bin_dir" 'Left your shell profile untouched, as asked.'
	else
		local profile
		# Detecting nothing is an ordinary outcome, not a failure: the `|| true`
		# keeps it from aborting the run under `set -e`, which would abandon an
		# already-installed binary over a profile this script chose not to guess at.
		profile="$(detect_profile || true)"
		if [ -n "$profile" ]; then
			wire_path "$bin_dir" "$profile"
			printf '\nOpen a new terminal, or run: source %s\n' "$profile"
		else
			print_manual_path_instructions "$bin_dir" 'No shell profile was detected, so none was edited.'
		fi
	fi

	printf '\nThe WSO2 CLI %s is installed. Run: wso2 --help\n' "$version"
}

main "$@"
