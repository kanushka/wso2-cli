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

package auth_test

import (
	"sync"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/auth/session"
)

// serializedKeyring guards go-keyring's in-memory mock, whose map is not safe
// for concurrent use. A test that stands a goroutine in for a second
// invocation reads and writes it at once; the rotation lock that orders those
// in a real run is a file lock the race detector cannot see.
type serializedKeyring struct{ mu sync.Mutex }

func (k *serializedKeyring) Get(service, user string) (string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	return keyring.Get(service, user)
}

func (k *serializedKeyring) Set(service, user, password string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return keyring.Set(service, user, password)
}

func (k *serializedKeyring) Delete(service, user string) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	return keyring.Delete(service, user)
}

func serializeKeyring(t *testing.T) {
	t.Helper()
	t.Cleanup(session.UseSecureStore(&serializedKeyring{}))
}
