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

// Command launchcanary records that it ran, and does nothing else.
//
// A test that a module was rejected "before launch" is only as good as its
// evidence that no module started. Installing this as the executable turns
// that into a fact on disk: if the shell ever starts what it should have
// refused, the marker is there afterwards.
//
// It records itself beside its own executable, because the shell launches a
// module with no arguments and an environment sanitized to nothing, so there is
// no other place a test could agree on. Being an ordinary Go program rather
// than a shell script, it is a canary on every platform the shell runs on.
package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// Marker is the file this canary leaves behind, beside its own executable.
const Marker = "canary-was-launched"

func main() {
	executable, err := os.Executable()
	if err != nil {
		// A canary that cannot record itself must not look like one that was
		// never launched.
		fmt.Fprintf(os.Stderr, "launchcanary: cannot locate this executable: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(executable), Marker), nil, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "launchcanary: cannot record the launch: %v\n", err)
		os.Exit(1)
	}
}
