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

package output

import (
	"fmt"
	"io"
)

// ModuleDiagnostics writes what a module reported on its standard error.
//
// Diagnostics always go to the shell's standard error, whatever the output
// mode, so a command's structured output stays parseable even when the module
// was noisy. Each line is attributed to the module so a user can tell the
// shell's own warnings from a module's.
// A failed write to the diagnostics stream is discarded rather than returned,
// as it is everywhere else in this package: the only place to report it would
// be the stream that just failed, and it must not turn a command that otherwise
// succeeded into a failure.
func ModuleDiagnostics(w io.Writer, namespace string, lines []string, truncated bool) {
	for _, line := range lines {
		_, _ = fmt.Fprintf(w, "%s: %s\n", namespace, line)
	}
	if truncated {
		_, _ = fmt.Fprintf(w, "%s: further diagnostics were discarded because the module exceeded the shell's limit\n",
			namespace)
	}
}
