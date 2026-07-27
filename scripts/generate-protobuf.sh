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

# Regenerates the Go types of the module contract from sdk/proto.
#
# The generated files are committed, so this script is a maintainer step rather
# than a build step: a clean checkout builds and tests without a Protobuf
# toolchain and without network access. It needs both while it runs, because it
# fetches a pinned buf and a remote code-generation plugin.

set -eu

BUF_VERSION=v1.47.2

repo_root=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$repo_root/sdk"

echo "Linting the module contract schema"
GOWORK=off go run "github.com/bufbuild/buf/cmd/buf@$BUF_VERSION" lint

echo "Generating Go types into sdk/protocol/contractv1"
GOWORK=off go run "github.com/bufbuild/buf/cmd/buf@$BUF_VERSION" generate

echo "Applying the license header to the generated files"
"$repo_root/scripts/apply-license-header.sh" "$repo_root/sdk/protocol/contractv1"

echo "Done. Review and commit the regenerated files."
