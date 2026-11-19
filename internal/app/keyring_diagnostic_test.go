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

package app_test

import (
	"errors"
	"strings"
	"testing"

	keyring "github.com/zalando/go-keyring"

	"github.com/wso2/wso2-cli/internal/exit"
)

const backendAnswer = "data passed to Set was too big"

func TestVerboseNamesTheSecureStoreBackendsOwnError(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc("https://issuer.example.test"))
	keyring.MockInitWithError(errors.New(backendAnswer))

	if code := shell.Run([]string{"whoami", "--verbose"}); code != exit.AuthPolicy {
		t.Fatalf("exit %d, want %d: %s", code, exit.AuthPolicy, errOut)
	}
	if !strings.Contains(errOut.String(), "auth.keyring_unavailable") || !strings.Contains(errOut.String(), backendAnswer) {
		t.Errorf("--verbose does not name the backend's own error:\n%s", errOut)
	}
}

func TestTheSecureStoreBackendsOwnErrorStaysOutOfTheRefusal(t *testing.T) {
	shell, _, errOut := newLoginShell(t)
	installLogin(t, shell, browserDoc("https://issuer.example.test"))
	keyring.MockInitWithError(errors.New(backendAnswer))

	shell.Run([]string{"whoami"})
	if strings.Contains(errOut.String(), backendAnswer) {
		t.Errorf("the refusal carries the backend's own error:\n%s", errOut)
	}
}
