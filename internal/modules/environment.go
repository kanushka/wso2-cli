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

package modules

import (
	"os"
	"runtime"
)

// SanitizedEnvironment builds a module process's environment from nothing.
//
// The shell decides what a module may see rather than filtering what it must
// not: a deny list would leak every variable nobody thought of. Only the
// entries an operating system needs to start a process at all are added back.
//
// It lives here rather than beside either caller because there are two, and a
// module process is launched twice in a module's life — once at install, to ask
// what commands it serves, and again for every invocation. Two copies of this
// rule would be two places for it to drift, and the one that drifted would be
// the one nobody was looking at.
func SanitizedEnvironment() []string {
	if runtime.GOOS != "windows" {
		return []string{}
	}
	// Windows cannot reliably start a process without these, and neither
	// carries user or credential data.
	var environment []string
	for _, name := range []string{"SYSTEMROOT", "SYSTEMDRIVE", "WINDIR"} {
		if value, present := os.LookupEnv(name); present {
			environment = append(environment, name+"="+value)
		}
	}
	return environment
}
