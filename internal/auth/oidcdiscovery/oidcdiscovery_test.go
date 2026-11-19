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

package oidcdiscovery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestARedirectTheShellCannotFollowIsNotUnreachable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "http://[::1")
		w.WriteHeader(http.StatusFound)
	}))
	defer server.Close()

	_, err := Discover(context.Background(), server.URL, "")
	var refused problem.Problem
	if !errors.As(err, &refused) || refused.Category != problem.CategoryAuthPolicy {
		t.Errorf("err = %v, want an auth_policy problem: the provider answered", err)
	}

	server.Close()
	_, err = Discover(context.Background(), server.URL, "")
	if !errors.As(err, &refused) || refused.Category != problem.CategoryUnreachable {
		t.Errorf("err = %v, want an unreachable problem: nothing answered", err)
	}
}
