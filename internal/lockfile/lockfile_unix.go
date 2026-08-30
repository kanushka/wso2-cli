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

//go:build unix

package lockfile

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// tryLock takes an exclusive advisory lock on file without blocking. It
// reports whether the lock was taken; a peer already holding it is contention,
// not an error, and is reported as (false, nil).
//
// The kernel ties the lock to the open file description and drops it when the
// process exits however it exits, so a lock held by a crashed process cannot
// outlive it.
func tryLock(file *os.File) (bool, error) {
	err := unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, unix.EWOULDBLOCK):
		return false, nil
	default:
		return false, err
	}
}
