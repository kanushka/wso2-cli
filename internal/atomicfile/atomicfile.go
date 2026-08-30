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

// Package atomicfile replaces a file's contents in one step.
//
// A write goes to a temporary file in the target's own directory and is renamed
// over the target, so a reader sees either the previous contents or the new
// ones and never a partial write, and a failed write leaves what was already
// there untouched. The temporary file is created beside the target rather than
// in the system temporary directory because a rename is only atomic within one
// filesystem.
//
// The package classifies nothing: it returns a wrapped filesystem error and
// leaves the typed problem to the caller, because the module store and the
// context document describe the same failure to a user in different words.
package atomicfile

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Write replaces the file at path with data, atomically, at the given mode.
//
// The mode is set on the temporary file before the rename, so the file never
// appears at the target under the 0600 os.CreateTemp opens at and then widens.
// A document that must not be world-readable is never briefly readable, and a
// document that must be world-readable is never briefly private.
//
// The target's directory must already exist: a caller that knows the directory
// also knows what mode to create it at, and this package does not guess.
func Write(path string, data []byte, mode fs.FileMode) error {
	temporary, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+"-")
	if err != nil {
		return fmt.Errorf("atomicfile: cannot create a temporary file beside %s: %w", path, err)
	}
	// Chmod is a no-op on Windows, where the mode carries no meaning the
	// filesystem enforces. Nothing here depends on it succeeding there.
	err = temporary.Chmod(mode)
	if err == nil {
		_, err = temporary.Write(data)
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(temporary.Name(), path)
	}
	if err != nil {
		_ = os.Remove(temporary.Name())
		return fmt.Errorf("atomicfile: cannot write %s: %w", path, err)
	}
	return nil
}
