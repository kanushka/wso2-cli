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

package rpc

import (
	"crypto/rand"
	"encoding/hex"
)

// invocationIDBytes is the entropy behind one invocation identifier.
const invocationIDBytes = 16

// NewInvocationID mints the identifier that binds one invocation's messages
// together.
//
// It is generated from a cryptographic source rather than a counter or a
// timestamp because the broker binds issued access to it: a guessable
// invocation identifier would let access granted for one command be presented
// as another's.
//
// The shell mints one per command, before it launches anything, so the broker
// and the module session agree about which invocation is in progress.
func NewInvocationID() (string, error) {
	raw := make([]byte, invocationIDBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}
