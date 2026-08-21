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

# Assembles everything the catalog origin serves into one directory: the
# install and uninstall scripts, a landing page, and the module catalog
# generated from the tags that exist.
#
# Both workflows that publish to that origin run this same script, because a
# deployment replaces the whole site. A deployment that assembled only half of
# it would take the other half down: publishing a script change would remove
# the catalog, and publishing a catalog would remove the install scripts the
# README tells people to use.
#
# The catalog is regenerated rather than carried forward for the same reason it
# is generated at all: it is a function of the tags that exist, so nothing that
# is published can disagree with what was released.
#
#	./scripts/assemble-site.sh site
#
# Reading the release page needs gh authenticated, which in a workflow is the
# run's own token in GH_TOKEN.
set -eu

site="${1:-site}"
root="$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd)"

mkdir -p "${site}"
# The generator is run from the repository root, so a relative site path would
# otherwise be resolved against the wrong directory.
site="$(CDPATH='' cd -- "${site}" && pwd)"
cp "${root}/scripts/install.sh" "${root}/scripts/install.ps1" \
	"${root}/scripts/uninstall.sh" "${root}/scripts/uninstall.ps1" "${site}/"

# A landing page, so someone who opens the host in a browser finds out what
# these files are rather than a directory listing or a 404.
cat >"${site}/index.html" <<'HTML'
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Install the WSO2 CLI</title>
</head>
<body>
<h1>Install the WSO2 CLI</h1>
<p>macOS, Linux, and WSL:</p>
<pre><code>curl -fsSL https://wso2.github.io/wso2-cli/install.sh | bash</code></pre>
<p>Windows:</p>
<pre><code>iwr https://wso2.github.io/wso2-cli/install.ps1 -useb | iex</code></pre>
<p>
These scripts download a published release, verify it against the
checksum file published beside it, and install the binary under your WSO2
state root. Read either one before running it: they are plain text at the
URLs above.
</p>
<p>
This host also serves the module catalog the CLI reads to install and update
product modules: <a href="index.json">index.json</a> and one file per product
namespace under <code>modules/</code>. Both are generated from the tags that
exist.
</p>
<p>
<a href="https://github.com/wso2/wso2-cli/blob/main/docs/guides/installing.md">Installation guide</a>,
including how to install without piping a script to a shell, how to pin a
version, and how to uninstall.
</p>
</body>
</html>
HTML

input="$(mktemp)"
trap 'rm -f "${input}"' EXIT
(cd "${root}" && go run ./cmd/wso2-catalog-input -repo . -out "${input}")
(cd "${root}" && go run ./cmd/wso2-catalog -input "${input}" -out "${site}" -repo .)

echo "assembled the site into ${site}"
