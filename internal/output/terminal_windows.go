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

//go:build windows

package output

import "golang.org/x/sys/windows"

// isTerminal reports whether fd names a console.
//
// GetConsoleMode succeeds only for a console handle; a pipe, a regular file,
// or a closed or nonsense handle fails, which this reports as "not a
// terminal" rather than propagating.
func isTerminal(fd uintptr) bool {
	var mode uint32
	return windows.GetConsoleMode(windows.Handle(fd), &mode) == nil
}

// rendersColor reports whether the console behind fd renders ANSI escapes,
// turning its virtual-terminal processing on when it is off. A console that
// refuses it, as one before Windows 10 does, gets plain output rather than
// raw escape codes.
func rendersColor(fd uintptr) bool {
	handle := windows.Handle(fd)
	var mode uint32
	if windows.GetConsoleMode(handle, &mode) != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	return windows.SetConsoleMode(handle, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING) == nil
}
