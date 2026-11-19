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

package app

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// A refusal whose recovery a command extends keeps the error it wraps, so
// --verbose still names the secure store backend's own answer.
func TestExtendedRecoveriesKeepTheWrappedCause(t *testing.T) {
	backend := errors.New("data passed to Set was too big")
	refusal := func() error {
		return fmt.Errorf("%w: %w", problem.New(problem.CategoryAuthPolicy, "auth.keyring_unavailable",
			"the OS secure store is not available to the shell").WithRecovery("Enable the OS keychain."), backend)
	}
	for name, extended := range map[string]error{
		"org use": orgUseUnfinished(refusal(), "cloud-dev", ""),
		"login": extendIncompleteLoginRecovery(refusal(),
			[]contexts.ProductAccess{{Namespace: "apim"}}, contexts.ProductAccess{Namespace: "iam"}),
	} {
		var reported problem.Problem
		if !errors.As(extended, &reported) || !strings.HasPrefix(reported.Recovery, "Enable the OS keychain. ") {
			t.Errorf("%s: the recovery was not extended: %v", name, extended)
		}
		if !errors.Is(extended, backend) {
			t.Errorf("%s: the backend's error left the chain: %v", name, extended)
		}
		if strings.Count(extended.Error(), "auth.keyring_unavailable") != 1 {
			t.Errorf("%s: the refusal repeats itself: %v", name, extended)
		}
	}
}
