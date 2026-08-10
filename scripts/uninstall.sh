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

# Removes what scripts/install.sh added on macOS, Linux, and WSL.
#
#	bash uninstall.sh              # remove the binary and the profile block
#	bash uninstall.sh --purge      # also remove configuration and credentials
#
# It removes the binary, the directory the installer created for it, and the
# delimited block the installer appended to a shell profile. It does not remove
# configuration, contexts, or credentials unless asked: removing a binary is not
# the same decision as abandoning a setup, and silently destroying the second
# would be the worse default.
#
# Running it when nothing is installed is not a failure. It reports what it found
# and exits successfully, which is also what makes it usable to clean up after an
# install that failed halfway.
#
# The block markers and paths here must match scripts/install.sh exactly. They are
# repeated rather than shared because each script is fetched and run on its own,
# so neither can source the other.

set -euo pipefail

BLOCK_BEGIN='# >>> wso2 cli >>>'
BLOCK_END='# <<< wso2 cli <<<'

PURGE=0
for argument in "$@"; do
	case "$argument" in
	--purge) PURGE=1 ;;
	-h | --help)
		printf 'Usage: uninstall.sh [--purge]\n\n'
		printf '  --purge  Also remove configuration, contexts, and credentials.\n'
		exit 0
		;;
	*)
		printf 'error: unknown option: %s\n' "$argument" >&2
		exit 1
		;;
	esac
done

state_root="${WSO2_HOME:-$HOME/.wso2}"
bin_dir="${state_root}/bin"
removed=0

# The binary and any staging file an interrupted install left beside it.
if [ -e "${bin_dir}/wso2" ]; then
	rm -f "${bin_dir}/wso2"
	printf 'Removed %s\n' "${bin_dir}/wso2"
	removed=1
fi
rm -f "${bin_dir}"/.wso2.install.* 2>/dev/null || true

# Only if it is empty. A directory holding something this installer did not put
# there is not this script's to delete.
if [ -d "$bin_dir" ] && [ -z "$(ls -A "$bin_dir" 2>/dev/null)" ]; then
	rmdir "$bin_dir"
	printf 'Removed %s\n' "$bin_dir"
fi

# Every profile is checked, not only the one this shell would be wired in: the
# install may have run under a different shell, and a block left behind would go
# on putting a directory that no longer exists on PATH.
for profile in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshrc" "$HOME/.zprofile" "$HOME/.profile"; do
	[ -f "$profile" ] || continue
	grep -qF "$BLOCK_BEGIN" "$profile" || continue

	staged="${profile}.wso2-uninstall.$$"
	# Only the lines between the markers go. Everything else is written back
	# byte for byte, through a temporary file beside the profile so an interrupted
	# run cannot truncate it.
	awk -v begin="$BLOCK_BEGIN" -v end="$BLOCK_END" '
		$0 == begin { inside = 1; next }
		$0 == end { inside = 0; next }
		!inside { print }' "$profile" >"$staged"
	mv "$staged" "$profile"
	printf 'Removed the wso2 block from %s\n' "$profile"
	removed=1
done

if [ "$PURGE" -eq 1 ]; then
	if [ -d "$state_root" ]; then
		rm -rf "$state_root"
		printf 'Removed %s, including configuration and credentials.\n' "$state_root"
		removed=1
	fi
else
	# Named explicitly rather than left implicit: someone who wanted everything
	# gone needs to know that something is still there and how to remove it.
	if [ -d "$state_root" ]; then
		printf '\nLeft %s in place, with your contexts and credentials.\n' "$state_root"
		printf 'Remove it too with: bash uninstall.sh --purge\n'
	fi
fi

if [ "$removed" -eq 0 ]; then
	printf 'Nothing to remove: no wso2 installation was found under %s.\n' "$state_root"
else
	printf '\nOpen a new terminal so the PATH change takes effect.\n'
fi
