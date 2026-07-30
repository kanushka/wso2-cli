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

# Runs the architecture-proof acceptance gate.
#
# This is the whole of what "the proof passes" means, in one command that a
# contributor and CI both run. It builds the three independently versioned
# modules from the checkout it is run in and then runs every test layer the
# proof depends on, ending with the black-box runs that drive the built shell
# and the built module through the same external seam a user does.
#
# See docs/plans/first-cli-vertical-slice.md for what each layer proves, and
# docs/plans/architecture-proof-review.md for the review this gate supports.
#
# It needs a Go toolchain and a warm or reachable module cache. It needs no
# Protobuf toolchain, no network catalog, no WSO2 credentials, and no real
# product service: everything the proof authenticates against is a fixture this
# repository builds.

set -eu

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root"

# No acceptance run may read or write the developer's real WSO2 state. Every
# test roots the shell in its own temporary directory, and this is the second
# lock on that: an ambient WSO2_HOME cannot reach the runs, and a run that
# somehow ignored its own root would land here rather than in ~/.wso2.
#
# Every other WSO2_ variable is cleared for the same reason. The credential the
# proof uses is supplied by the harness under a name it owns, so an ambient one
# can only make a run pass for a reason the gate did not intend.
isolated_state=$(mktemp -d 2>/dev/null || mktemp -d -t wso2-acceptance)
# Build output goes somewhere else entirely. The state root is meant to stay
# empty unless a run wrote to it wrongly, and an executable dropped there by
# this script would be the first thing to hide that.
build_output=$(mktemp -d 2>/dev/null || mktemp -d -t wso2-acceptance-build)
trap 'rm -rf "$isolated_state" "$build_output"' EXIT INT TERM
for variable in $(env | sed -n 's/^\(WSO2_[A-Za-z0-9_]*\)=.*/\1/p'); do
	unset "$variable"
done
WSO2_HOME="$isolated_state"
export WSO2_HOME

stage() {
	printf '\n== %s ==\n' "$1"
}

# 1. A clean checkout builds the three independently versioned modules.
#
# Each is built the way it is released: the shell and the reference module in
# this workspace, and the SDK with workspace composition disabled, because a
# published SDK is consumed by modules that have no workspace at all.
stage 'Build the shell'
go build ./...

stage 'Build the public SDK without workspace composition'
(cd sdk && GOWORK=off go build ./...)

stage 'Build the reference module'
(cd examples/reference-module && go build -o "$build_output/" ./...)

# 2. The SDK's own boundary and contract tests, again without the workspace.
stage 'Test the public SDK'
(cd sdk && GOWORK=off go test ./...)

# 3. The reference module's tests, which exercise the SDK as a module author
#    meets it rather than as this repository composes it.
stage 'Test the reference module'
(cd examples/reference-module && go test ./...)

# 4. The shell's build boundaries, unit tests, and integration tests.
#
# They run before the black-box layer because they fail faster and say more:
# an acceptance run that fails because the frame codec is broken reports a
# module that would not answer, which is a long way from the defect.
stage 'Test the shell'
go test ./cmd/... ./internal/...

# 5. The black-box acceptance runs: the built shell and the built module, an
#    isolated managed store and context, the local status service, and the
#    canary credential no run may disclose.
#
# This names every package rather than only the acceptance tree, so a package
# added anywhere in the shell module is tested by the gate whether or not
# anyone remembered to list it here. The packages of stage 4 are unchanged
# since they ran, so the test cache answers for them and only the black-box
# runs cost anything.
stage 'Run the black-box acceptance suite'
go test ./...

printf '\nThe architecture-proof acceptance gate passed.\n'
