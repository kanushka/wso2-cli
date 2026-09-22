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
#	bash uninstall.sh --purge      # also remove configuration and contexts
#
# It removes the binary, the directory the installer created for it, the
# delimited block the installer appended to a shell profile (tab completion
# included), and the fish completion file. It does not remove configuration or
# contexts unless asked: removing a binary is not the same decision as abandoning
# a setup, and silently destroying the second would be the worse default.
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
		printf '  --purge  Also remove configuration and contexts. Sessions in the OS\n'
		printf '           secure store are not touched: run logout first.\n'
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

# The binary, under the name the installer recorded (an install from before
# the record was named wso2), and any staging file an interrupted install left
# beside it.
cli_name="$(cat "${bin_dir}/.cli-name" 2>/dev/null || printf 'wso2')"
case "$cli_name" in
'' | */* | . | ..) cli_name=wso2 ;;
esac
if [ -e "${bin_dir}/${cli_name}" ]; then
	rm -f "${bin_dir}/${cli_name}"
	printf 'Removed %s\n' "${bin_dir}/${cli_name}"
	removed=1
fi
rm -f "${bin_dir}/.cli-name" "${bin_dir}"/.wso2.install.* 2>/dev/null || true

# Only if it is empty. A directory holding something this installer did not put
# there is not this script's to delete.
if [ -d "$bin_dir" ] && [ -z "$(ls -A "$bin_dir" 2>/dev/null)" ]; then
	rmdir "$bin_dir"
	printf 'Removed %s\n' "$bin_dir"
	# Counted, so the summary cannot end with "nothing to remove" after saying
	# what it removed.
	removed=1
fi

# Every profile is checked, not only the one this shell would be wired in: the
# install may have run under a different shell, and a block left behind would go
# on putting a directory that no longer exists on PATH.
for profile in "$HOME/.bashrc" "$HOME/.bash_profile" "$HOME/.zshrc" "$HOME/.zprofile" "$HOME/.profile"; do
	[ -f "$profile" ] || continue
	grep -qF -e "$BLOCK_BEGIN" -e "$BLOCK_END" "$profile" || continue

	# Exactly one begin marker, then exactly one end marker after it. Any other
	# shape — no end, end before begin, a second begin or end — would make the
	# rewrite below treat the user's own configuration as inside the block and
	# drop it. Refusing to guess, leaving the file as it is, and saying why is the
	# only safe answer. This is the same rule uninstall.ps1 applies, narrowed to a
	# single block because the installer only ever writes one.
	problem="$(awk -v begin="$BLOCK_BEGIN" -v end="$BLOCK_END" '
		$0 == begin { begins++; if (!first_begin) first_begin = NR }
		$0 == end { ends++; if (!first_end) first_end = NR }
		END {
			if (begins == 0 && ends == 0) exit
			if (begins > 1) print "more than one wso2 block start marker"
			else if (ends > 1) print "more than one wso2 block end marker"
			else if (begins == 0) print "a wso2 block end marker but no start marker"
			else if (ends == 0) print "the wso2 block start but no end marker"
			else if (first_end < first_begin) print "the wso2 block end marker before its start marker"
		}' "$profile")"
	if [ -n "$problem" ]; then
		printf 'warning: %s has %s.\n' "$profile" "$problem" >&2
		printf 'Left it alone rather than guessing where the block is. Remove these lines by hand:\n' >&2
		printf '  %s ... %s\n' "$BLOCK_BEGIN" "$BLOCK_END" >&2
		continue
	fi
	# Markers that only appear as part of longer lines are not a block.
	grep -qxF "$BLOCK_BEGIN" "$profile" || continue

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

# The fish completion file `completion install` writes, recognised by the block
# marker it carries. A file of the same name without it is the user's own.
fish_completion="${XDG_CONFIG_HOME:-$HOME/.config}/fish/completions/${cli_name}.fish"
if [ -f "$fish_completion" ] && grep -qF "$BLOCK_BEGIN" "$fish_completion"; then
	rm -f "$fish_completion"
	printf 'Removed %s\n' "$fish_completion"
	removed=1
fi

if [ "$PURGE" -eq 1 ]; then
	if [ -d "$state_root" ]; then
		rm -rf "$state_root"
		printf 'Removed %s, including configuration and contexts.\n' "$state_root"
		printf 'Sessions in the OS secure store are not touched: run logout first.\n'
		removed=1
	fi
else
	# Named explicitly rather than left implicit: someone who wanted everything
	# gone needs to know that something is still there and how to remove it.
	if [ -d "$state_root" ]; then
		printf '\nLeft %s in place, with your contexts and preferences.\n' "$state_root"
		printf 'Remove it too with: bash uninstall.sh --purge\n'
	fi
fi

if [ "$removed" -eq 0 ]; then
	printf 'Nothing to remove: no WSO2 CLI installation was found under %s.\n' "$state_root"
else
	printf '\nOpen a new terminal so the PATH change takes effect.\n'
fi
