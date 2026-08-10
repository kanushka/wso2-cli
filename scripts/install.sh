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

# The paths the cleanup removes are global rather than local to main, because the
# EXIT trap runs after main's locals have gone out of scope: a local would leave
# the trap with an unset name and the files on disk.
#
# STAGED_BINARY is the partly-installed binary beside its final path. It is
# cleaned up too, because it lands in the user's own bin directory rather than in
# a temporary one, so an interrupted run would otherwise leave it there.
TEMP_DIR=''
STAGED_BINARY=''

cleanup() {
	rm -rf "${TEMP_DIR:-}"
	rm -f "${STAGED_BINARY:-}"
}

# A signal has to end the run. Cleaning up and then carrying on would install
# from an interrupted download, and bash resumes after a signal handler unless
# the handler exits.
on_signal() {
	cleanup
	printf '\ninterrupted; nothing was installed.\n' >&2
	exit 130
}

fail() {
	printf 'error: %s\n' "$1" >&2
	exit 1
}

# require_tools fails before anything is downloaded when a tool this script
# cannot work without is missing, naming the tool rather than failing later with
# whatever error the missing command happens to produce.
require_tools() {
	local tool
	for tool in curl mktemp uname grep awk tr; do
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
		# Each failure is caught here rather than left to `set -e`, which would
		# abort with curl's own exit status and no indication of which step failed.
		local listing
		listing="$(curl -fsSL "$RELEASE_API_URL")" ||
			fail "could not read the release listing at ${RELEASE_API_URL}."
		tag="$(printf '%s' "$listing" | awk '
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
		resolved="$(curl -fsSL -o /dev/null -w '%{url_effective}' "${RELEASE_BASE_URL}/latest")" ||
			fail "could not reach ${RELEASE_BASE_URL}/latest to find the newest release."
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

	# The filename is compared exactly rather than searched for. A substring match
	# would accept the hash of any longer name that ends in this one — a
	# `<archive>.sig` line listed first would hand over the wrong hash entirely —
	# and that is a verification bypass, not a cosmetic difference. The leading
	# marker sha256sum writes for binary mode is stripped before comparing.
	expected="$(awk -v want="$archive" '
		{ name = $2; sub(/^\*/, "", name) }
		name == want { print $1; exit }' "${directory}/checksums.txt" || true)"
	[ -n "$expected" ] ||
		fail "checksums.txt does not list ${archive}, so the download cannot be verified."

	if command -v sha256sum >/dev/null 2>&1; then
		actual="$(sha256sum "${directory}/${archive}" | awk '{print $1}')"
	elif command -v shasum >/dev/null 2>&1; then
		actual="$(shasum -a 256 "${directory}/${archive}" | awk '{print $1}')"
	else
		fail 'this installer needs sha256sum or shasum to verify the download, and found neither.'
	fi

	# Hex case is not part of the value, so it is normalised away rather than left
	# to turn a matching digest into a spurious refusal.
	expected="$(printf '%s' "$expected" | tr '[:upper:]' '[:lower:]')"
	actual="$(printf '%s' "$actual" | tr '[:upper:]' '[:lower:]')"

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

# wire_path exports the state root and puts the binary directory on PATH for
# future shells.
#
# The state root is exported, not just used: an installation under a non-default
# WSO2_HOME would otherwise leave the installed shell reading its state from the
# default root, so the binary and its state would disagree about where they live.
#
# The block is delimited and greppable so that a second run can recognise its own
# work rather than appending a duplicate, and so that removing it later is an
# exact operation rather than a judgement call about which lines were ours.
#
# An existing block whose paths differ is replaced rather than left alone. Leaving
# it would silently keep a previous install's directory on PATH, which is how a
# re-run into a new state root ends up running the old binary.
wire_path() {
	local state_root="$1" bin_dir="$2" profile="$3"

	if grep -qF "$BLOCK_BEGIN" "$profile" 2>/dev/null; then
		if grep -qF "\"${bin_dir}:\$PATH\"" "$profile" 2>/dev/null; then
			printf 'PATH is already wired in %s.\n' "$profile"
			return
		fi
		remove_block "$profile"
		printf 'Replaced an earlier wso2 block in %s.\n' "$profile"
	fi

	{
		printf '\n%s\n' "$BLOCK_BEGIN"
		printf 'export WSO2_HOME="%s"\n' "$state_root"
		printf 'export PATH="%s:$PATH"\n' "$bin_dir"
		printf '%s\n' "$BLOCK_END"
	} >>"$profile"
	printf 'Added %s to PATH in %s.\n' "$bin_dir" "$profile"
}

# remove_block deletes this installer's block from a profile, in place, leaving
# every other line byte for byte as it was. The rewrite goes through a temporary
# file beside the profile so an interrupted run cannot truncate it.
remove_block() {
	local profile="$1" staged
	staged="${profile}.wso2-install.$$"
	awk -v begin="$BLOCK_BEGIN" -v end="$BLOCK_END" '
		$0 == begin { inside = 1; next }
		$0 == end { inside = 0; next }
		!inside { print }' "$profile" >"$staged"
	mv "$staged" "$profile"
}

print_manual_path_instructions() {
	local state_root="$1" bin_dir="$2" reason="$3"
	printf '\n%s\n' "$reason"
	printf 'Add these lines to your shell profile to run wso2 by name:\n\n'
	printf '    export WSO2_HOME="%s"\n' "$state_root"
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

	# The profile block quotes these paths, so a root containing a quote, a dollar
	# or a backslash would write a line that means something other than the path it
	# came from. Refusing beats writing a profile that misbehaves later.
	case "$state_root" in
	*'"'* | *'$'* | *'\'* | *'`'*)
		fail "the state root ${state_root} contains a character this installer cannot safely write into a shell profile."
		;;
	esac
	archive="wso2-cli-${version}-${os}-${arch}.${extension}"
	url="${RELEASE_BASE_URL}/download/${version}/${archive}"

	printf 'Installing the WSO2 CLI %s for %s/%s.\n' "$version" "$os" "$arch"

	# Everything downloaded lands in a temporary directory that is removed however
	# this script exits, so a failed verification leaves nothing behind to run.
	TEMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t wso2-install)"
	trap cleanup EXIT
	trap on_signal INT TERM

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
	# written one would be worse, so the new binary is staged beside its final path
	# and then renamed over it. The staging name carries this run's process id, so
	# two runs at once cannot stage onto each other, and it is inside the
	# destination directory so the rename is atomic rather than a copy across
	# filesystems.
	STAGED_BINARY="${bin_dir}/.wso2.install.$$"
	rm -rf "$STAGED_BINARY"
	mv "${TEMP_DIR}/unpacked/wso2" "$STAGED_BINARY"
	chmod +x "$STAGED_BINARY"
	mv "$STAGED_BINARY" "${bin_dir}/wso2"
	STAGED_BINARY=''
	printf 'Installed %s\n' "${bin_dir}/wso2"

	if [ -n "${WSO2_CLI_NO_PROFILE:-}" ]; then
		print_manual_path_instructions "$state_root" "$bin_dir" \
			'Left your shell profile untouched, as asked.'
	else
		local profile
		# Detecting nothing is an ordinary outcome, not a failure: the `|| true`
		# keeps it from aborting the run under `set -e`, which would abandon an
		# already-installed binary over a profile this script chose not to guess at.
		profile="$(detect_profile || true)"
		if [ -n "$profile" ]; then
			wire_path "$state_root" "$bin_dir" "$profile"
			printf '\nOpen a new terminal, or run: source %s\n' "$profile"
		else
			print_manual_path_instructions "$state_root" "$bin_dir" \
				'No shell profile was detected, so none was edited.'
		fi
	fi

	printf '\nThe WSO2 CLI %s is installed. Run: wso2 --help\n' "$version"
}

main "$@"
