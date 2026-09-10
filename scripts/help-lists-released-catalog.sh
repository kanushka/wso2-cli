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

# Proves a shell binary's help page lists every product in the catalog copy it
# was built with, each marked not installed.
#
# The shell reads a copy it cannot decode as one naming no product, so a
# mangled injection would otherwise ship a help page that silently names
# nothing. Help lists only products with a stable release, so only those are
# required. The state root is redirected to an empty directory, so nothing is
# installed and every listed product has to be marked.
#
#	./scripts/help-lists-released-catalog.sh <wso2 binary> <base64 index.json>
set -eu

binary="$1"
encoded="$2"

state="$(mktemp -d)"
trap 'rm -rf "${state}"' EXIT

help="$(WSO2_HOME="${state}" "${binary}" help)"
# jq decodes the copy, because base64 spells its decode flag differently on
# GNU and BSD systems and this runs on both.
namespaces="$(printf '%s' "${encoded}" | jq -Rr '@base64d' |
	jq -r '.modules[] | select(any(.channels[]?; .channel == "stable")) | .namespace')"
if [ -z "${namespaces}" ]; then
	echo "the catalog copy names no product with a stable release, so there is nothing to check" >&2
	exit 1
fi
for namespace in ${namespaces}; do
	if ! printf '%s\n' "${help}" | grep -qE "^   ${namespace} .*\(not installed\)$"; then
		echo "the help page does not list ${namespace} from its catalog copy:" >&2
		printf '%s\n' "${help}" >&2
		exit 1
	fi
done
echo "the help page lists every product in its catalog copy: $(echo ${namespaces})"
