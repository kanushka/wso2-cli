#!/bin/sh
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

# Prints the module-contract protocol versions the currently released shell
# speaks, by asking the published binary rather than by reading this checkout.
#
# The gate compares a module against the shell a user can actually have, and
# between a protocol bump landing on the default branch and the shell release
# that carries it, the two are not the same thing. The published binary's answer
# comes from sdk/protocol all the same: the shell's release workflow refuses to
# publish a binary whose window is not the one the source declares.
#
# Nothing is printed when no shell has been released yet, which the caller reads
# as "fall back to what this checkout declares". Every other failure is fatal:
# reading an outage as "no shell is released" would open the gate.
#
#	./scripts/released-shell-protocols.sh
#
# Needs gh authenticated, which in a workflow is the run's own token in GH_TOKEN.
set -eu

repository="${GITHUB_REPOSITORY:-wso2/wso2-cli}"

# The newest stable shell release. Module tags are namespaced and the SDK's are
# prefixed, so a shell tag is the one that starts with "v".
tag="$(gh release list --repo "${repository}" --exclude-drafts --exclude-pre-releases \
	--limit 100 --json tagName \
	--jq '[.[] | select(.tagName | test("^v[0-9]"))][0].tagName // ""')"
if [ -z "${tag}" ]; then
	exit 0
fi

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

archive="wso2-cli-${tag}-linux-amd64.tar.gz"
gh release download "${tag}" --repo "${repository}" --pattern "${archive}" --dir "${work}"
tar -xzf "${work}/${archive}" -C "${work}"

# The state root is redirected so that reading the module inventory cannot touch
# the runner's home directory.
WSO2_HOME="${work}/state"
export WSO2_HOME
reported="$("${work}/wso2" version)"

window="$(printf '%s\n' "${reported}" | awk '$1 == "Protocol" { sub(/^Protocol[[:space:]]+/, ""); print }')"
if [ -z "${window}" ] || [ "${window}" = "unavailable" ]; then
	echo "the released shell ${tag} reported no protocol window" >&2
	exit 1
fi
# `wso2 version` renders the window for a reader, as "v2, v1". The gate parses
# it with sdk/protocol.ParseVersions, which takes plain integers and silently
# skips anything else -- so passing the display form through would hand the
# workflow an empty window rather than fail loudly. Drop the display "v".
window="$(printf '%s\n' "${window}" | sed 's/v\([0-9]\)/\1/g')"
printf '%s\n' "${window}"
