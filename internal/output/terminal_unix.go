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

//go:build linux || darwin || dragonfly || freebsd || netbsd || openbsd

package output

import "golang.org/x/sys/unix"

// isTerminal reports whether fd names a terminal device.
//
// It asks the kernel for the descriptor's line settings; a terminal answers,
// anything else — a pipe, a regular file, a closed or nonsense descriptor —
// returns an error, which this reports as "not a terminal" rather than
// propagating. The ioctl request number differs between Linux and the BSD
// family (including Darwin), which is why it comes from
// ioctlGetTermios rather than being written here.
//
// The build tag is the explicit list of GOOS values that have an
// ioctlGetTermios constant defined (terminal_linux.go, terminal_bsd.go), not
// the broader "unix" tag: that tag also covers solaris, illumos, and aix,
// which have no such constant and would fail to compile here. Narrowing it
// makes the exclusion a decision a reader can see rather than a build
// failure discovered on one of those platforms.
func isTerminal(fd uintptr) bool {
	_, err := unix.IoctlGetTermios(int(fd), ioctlGetTermios)
	return err == nil
}
