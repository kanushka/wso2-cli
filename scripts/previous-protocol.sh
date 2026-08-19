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

# Proves that a product module built against the previous protocol still runs
# under the shell built from this branch.
#
# The older half of the protocol window is a promise to users who have not
# updated the shell yet, and nothing in this repository breaks visibly when it
# is broken: the reference module in this checkout is always built against the
# SDK at head, so it always speaks the current protocol. This gate is the one
# run that reproduces a released module's dependency graph instead — the
# workspace dropped, the SDK resolved by version from the module proxy — and
# launches the result as a real subprocess under a real shell.
#
# Dropping the workspace is also what enforces the prohibition on committed
# replace directives: a module whose go.mod replaces the SDK never resolves a
# published one, so the rule is checked here rather than merely written down.
#
# This is a pull-request gate, and it is distinct from the release gate: this
# one catches "we broke the older protocol", while the release gate catches
# "this module cannot run on any shell that exists".
#
# It needs a Go toolchain and a reachable module proxy. It reports its own
# conclusion in full on the last line, so a reader of a CI log never has to
# infer what was proved.

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

work=$(mktemp -d 2>/dev/null || mktemp -d -t wso2-previous-protocol)
trap 'rm -rf "$work"' EXIT INT TERM

stage() {
	printf '\n== %s ==\n' "$1"
}

# 1. No go.mod in this repository may carry a replace directive.
#
# The check is here rather than in the static checks because this gate is what
# a replace directive breaks: it would silently pin the SDK to this checkout,
# and the run below would prove the current protocol twice over.
stage 'Check that no committed go.mod replaces a module'
# The loop is not a pipeline, so a go.mod this toolchain cannot read aborts the
# gate instead of reading as a file without replacements.
replacements=
for modfile in $(git ls-files '*go.mod'); do
	# A go.mod without replacements reports "Replace": null, so the match is
	# on the array rather than on the key.
	if go mod edit -json "$modfile" | grep -q '"Replace": *\['; then
		replacements="$replacements$modfile
"
	fi
done
if [ -n "$replacements" ]; then
	echo 'The following go.mod files carry a replace directive, which is prohibited:'
	echo "$replacements"
	echo 'Local composition belongs in go.work, which does not travel with a release.'
	exit 1
fi

# 2. The probe module reports the protocol generation of one SDK, named either
#    as this checkout or as a published version. Both answers come from the
#    same program, so "the generation this branch declares" and "the generation
#    that release declared" are measured the same way.
mkdir "$work/probe"
cat >"$work/probe/main.go" <<'PROBE'
package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/wso2/wso2-cli/sdk/protocol"
)

func main() {
	window := make([]string, 0, len(protocol.Window()))
	for _, version := range protocol.Window() {
		window = append(window, strconv.Itoa(version))
	}
	fmt.Printf("version=%s\nwindow=%s\n", protocol.Version, strings.Join(window, ","))
}
PROBE
cat >"$work/probe/go.mod" <<'PROBEMOD'
module wso2probe

go 1.25.0

require github.com/wso2/wso2-cli/sdk v0.0.0
PROBEMOD
cp "$work/probe/go.mod" "$work/probe/go.mod.template"

# probe SDK_VERSION prints the probe output for one SDK version, or for this
# checkout when given the literal "checkout". The replace directive the checkout
# form writes lives in a scratch module that is deleted with the temporary
# directory; nothing in the repository gains one.
probe() {
	cp "$work/probe/go.mod.template" "$work/probe/go.mod"
	rm -f "$work/probe/go.sum"
	if [ "$1" = checkout ]; then
		(cd "$work/probe" && go mod edit \
			-replace "github.com/wso2/wso2-cli/sdk=$repo_root/sdk")
	else
		(cd "$work/probe" && go mod edit \
			-require "github.com/wso2/wso2-cli/sdk@$1")
	fi
	# A probe that cannot resolve or build an SDK says so and stops the gate. A
	# release reported as speaking an unknown generation would be skipped over
	# silently, and skipping is the one outcome this gate cannot afford.
	(cd "$work/probe" && GOWORK=off GOFLAGS=-mod=mod go mod tidy && GOWORK=off go run .) ||
		{
			# Standard error, because the caller reads this function's standard
			# output: a diagnostic printed there would be captured and lost.
			printf 'The SDK named %s could not be built to read the protocol it declares.\n' \
				"$1" >&2
			exit 1
		}
}

stage 'Read the protocol window this branch declares'
branch_probe=$(probe checkout)
current=$(printf '%s\n' "$branch_probe" | sed -n 's/^version=//p')
window=$(printf '%s\n' "$branch_probe" | sed -n 's/^window=//p')
previous=$(printf '%s\n' "$window" | cut -s -d, -f2)
printf 'This branch declares protocol v%s and a window of v%s.\n' "$current" \
	"$(printf '%s' "$window" | sed 's/,/, v/g')"

if [ -z "$previous" ]; then
	printf '\nNOT APPLICABLE: this branch declares protocol v%s, the first '\
		"$current"
	printf 'generation,\nso there is no previous protocol to prove. Nothing was checked.\n'
	exit 0
fi

# 3. Resolve the newest published SDK whose protocol generation is the
#    predecessor of this branch's. It is the SDK a module still in the field was
#    built against, and asking the proxy rather than assuming a version is what
#    keeps this gate correct as the generation moves.
stage "Resolve the newest published SDK speaking protocol v$previous"
# The placeholder requirement is dropped first: it names a version that was
# never published, and the proxy cannot answer a question about the module
# while the module graph asks for a version that does not exist.
cp "$work/probe/go.mod.template" "$work/probe/go.mod"
(cd "$work/probe" && go mod edit -droprequire github.com/wso2/wso2-cli/sdk)
# An unreachable proxy is a failure rather than an empty answer. Reading it as
# "nothing is published" would turn every network blip into a green gate that
# checked nothing, which is exactly the reading this gate exists to prevent.
listing=$(cd "$work/probe" && GOWORK=off go list -m -versions \
	github.com/wso2/wso2-cli/sdk 2>&1) || {
	printf '\nFAILED: the module proxy could not be asked what the SDK has '
	printf 'published, so\nwhether protocol v%s still runs is unknown:\n%s\n' \
		"$previous" "$listing"
	exit 1
}
published=$(printf '%s\n' "$listing" | cut -s -d' ' -f2- | tr ' ' '\n' | sed '/^$/d')

resolved=
for candidate in $(printf '%s\n' "$published" | tail -r 2>/dev/null ||
	printf '%s\n' "$published" | tac); do
	generation=$(probe "$candidate" | sed -n 's/^version=//p')
	printf 'SDK %s declares protocol v%s\n' "$candidate" "$generation"
	if [ "$generation" = "$previous" ]; then
		resolved=$candidate
		break
	fi
done

if [ -z "$resolved" ]; then
	if [ -z "$published" ]; then
		printf '\nNOT ENFORCEABLE YET: no SDK version is published, so no module '
		printf 'in the field\nwas built against protocol v%s and there is nothing '\
			"$previous"
		printf 'to prove. The previous\nprotocol was NOT checked. This gate starts '
		printf 'proving it on the first SDK release.\n'
		exit 0
	fi
	printf '\nFAILED: protocol v%s is inside the window this branch declares '\
		"$previous"
	printf '(v%s), but\nno published SDK speaks it. Published SDK versions: %s.\n'\
		"$(printf '%s' "$window" | sed 's/,/, v/g')" \
		"$(printf '%s\n' "$published" | tr '\n' ' ')"
	exit 1
fi
printf 'Resolved SDK %s for protocol v%s.\n' "$resolved" "$previous"

# 4. Build the reference module against that published SDK with the workspace
#    dropped, in a copy outside the workspace. The require is edited rather
#    than committed because the committed graph is the local one: the module
#    depends on the SDK version that does not exist yet, and this is the run
#    that substitutes one that does.
stage "Build the reference module against SDK $resolved"
# The copy is of the working tree rather than of HEAD, so a contributor who
# runs this gate on an uncommitted change is told about that change.
module_source="$work/module"
cp -R examples/reference-module "$module_source"
module_binary="$work/wso2-module-reference"
(
	cd "$module_source"
	GOWORK=off go mod edit -require "github.com/wso2/wso2-cli/sdk@$resolved"
	GOWORK=off GOFLAGS=-mod=mod go mod tidy
	GOWORK=off go build -o "$module_binary" ./cmd/wso2-module-reference
) || {
	printf '\nFAILED: the reference module does not build against SDK %s, '\
		"$resolved"
	printf 'which is the\npublished SDK speaking protocol v%s.\n' "$previous"
	exit 1
}

# 5. Launch that module under the shell built from this branch. The conformance
#    run is a black-box acceptance test because only that layer launches a real
#    module subprocess under a real shell; the in-process test kit would prove
#    the SDK's server loop rather than the shell's handshake.
stage "Run the conformance suite for protocol v$previous"
WSO2_PREVIOUS_PROTOCOL_MODULE="$module_binary" \
	WSO2_PREVIOUS_PROTOCOL_SDK="$resolved" \
	WSO2_PREVIOUS_PROTOCOL_VERSION="$previous" \
	go test ./test/acceptance/ -count=1 -v \
	-run '^TestAModuleBuiltAgainstThePreviousProtocolSDKRunsUnderThisShell$' \
	>"$work/conformance.log" 2>&1 || {
	cat "$work/conformance.log"
	printf '\nFAILED: protocol v%s is broken. A reference module built against '\
		"$previous"
	printf 'SDK %s,\nthe published SDK speaking protocol v%s, does not run under '\
		"$resolved" "$previous"
	printf 'the shell built\nfrom this branch.\n'
	exit 1
}
cat "$work/conformance.log"

# A -run pattern that matches nothing reports a passing package, so the run has
# to prove it ran. Without this the gate goes green the day the test is renamed.
if ! grep -q -- '--- PASS: TestAModuleBuiltAgainstThePreviousProtocolSDKRunsUnderThisShell' \
	"$work/conformance.log"; then
	printf '\nFAILED: the conformance run for protocol v%s did not run. It is ' "$previous"
	printf 'named\nTestAModuleBuiltAgainstThePreviousProtocolSDKRunsUnderThisShell in\n'
	printf 'test/acceptance, and this gate is the only thing that launches it.\n'
	exit 1
fi

printf '\nPASSED: protocol v%s holds. The reference module built against SDK %s '\
	"$previous" "$resolved"
printf 'runs\nunder the shell built from this branch.\n'
