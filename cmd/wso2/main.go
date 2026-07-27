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

// Command wso2 is the WSO2 CLI shell.
//
// The shell owns shared policy and dispatches product commands to
// independently released product modules resolved from its managed module
// store.
package main

import (
	"os"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/output"
)

func main() {
	shell := app.Shell{
		Streams: output.Streams{Out: os.Stdout, Err: os.Stderr},
	}
	os.Exit(int(shell.Run(os.Args[1:])))
}
