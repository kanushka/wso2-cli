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

package oauthflow

import (
	"os"
	"os/exec"
	"runtime"
)

// NoBrowserEnvVar suppresses the browser-open attempt when it is set to a
// non-empty value. The authorization URL is printed either way, so suppressing
// the browser changes how a login starts and never whether it can finish.
const NoBrowserEnvVar = "WSO2_NO_BROWSER"

// Open opens a URL in the user's default browser, best effort.
//
// It starts the opener without waiting for it: the browser outlives the shell
// invocation, and a login must not depend on what the opener process does next.
// Every failure is the caller's to ignore.
func Open(target string) error {
	if os.Getenv(NoBrowserEnvVar) != "" {
		return nil
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", target).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", target).Start()
	default:
		return exec.Command("xdg-open", target).Start()
	}
}
