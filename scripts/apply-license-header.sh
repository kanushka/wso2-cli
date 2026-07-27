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

# Prepends the Apache-2.0 header to every Go file under the given directories
# that does not already carry it.
#
# Code generators do not emit the header, so regenerated files need it applied
# before internal/boundaries accepts them.

set -eu

if [ "$#" -eq 0 ]; then
	echo "usage: $0 <directory>..." >&2
	exit 2
fi

header=$(cat <<'EOF'
// Copyright (c) 2026, WSO2 LLC. (https://www.wso2.com).
//
// WSO2 LLC. licenses this file to you under the Apache License,
// Version 2.0 (the "License"); you may not use this file except
// in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing,
// software distributed under the License is distributed on an
// "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
// KIND, either express or implied.  See the License for the
// specific language governing permissions and limitations
// under the License.
EOF
)

for directory in "$@"; do
	find "$directory" -name '*.go' -type f | while read -r file; do
		if head -n 1 "$file" | grep -q '^// Copyright (c) 2026, WSO2 LLC\.'; then
			continue
		fi
		printf '%s\n\n' "$header" | cat - "$file" >"$file.licensed"
		mv "$file.licensed" "$file"
		echo "  added header to $file"
	done
done
