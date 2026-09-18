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

package problem_test

import (
	"errors"
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

func TestNewBuildsAProblemWithNoRecovery(t *testing.T) {
	built := problem.New(problem.CategoryUsage, "reference.bad_flag", "the flag is not recognized")

	if built.Category != problem.CategoryUsage {
		t.Errorf("category is %q, want %q", built.Category, problem.CategoryUsage)
	}
	if built.Code != "reference.bad_flag" {
		t.Errorf("code is %q, want %q", built.Code, "reference.bad_flag")
	}
	if built.Message != "the flag is not recognized" {
		t.Errorf("message is %q, want %q", built.Message, "the flag is not recognized")
	}
	if built.Recovery != "" {
		t.Errorf("New set a recovery of %q; want none", built.Recovery)
	}
}

func TestWithRecoveryReturnsACopyCarryingTheRecovery(t *testing.T) {
	base := problem.New(problem.CategoryUsage, "reference.bad_flag", "the flag is not recognized")

	withRecovery := base.WithRecovery("Run wso2 reference --help to see the available flags.")

	if withRecovery.Recovery != "Run wso2 reference --help to see the available flags." {
		t.Errorf("recovery is %q, want the guidance text", withRecovery.Recovery)
	}
	if base.Recovery != "" {
		t.Error("WithRecovery mutated the receiver: base now carries a recovery")
	}
}

func TestErrorFormatsTheCategoryCodeAndMessage(t *testing.T) {
	built := problem.New(problem.CategoryAuthPolicy, "reference.access_denied", "access was denied")

	want := "auth_policy: reference.access_denied: access was denied"
	if got := built.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestProblemTravelsAsAnOrdinaryError(t *testing.T) {
	// A handler returns a Problem as an error, and the shell recovers it with
	// errors.As, so Problem must satisfy the error interface by value.
	var err error = problem.New(problem.CategoryModuleProcess, "reference.panic", "the module panicked")

	var typed problem.Problem
	if !errors.As(err, &typed) {
		t.Fatal("errors.As could not recover the Problem from the error")
	}
	if typed.Code != "reference.panic" {
		t.Errorf("recovered problem code is %q, want %q", typed.Code, "reference.panic")
	}
}

func TestCategoriesAreDistinctStableStrings(t *testing.T) {
	categories := map[problem.Category]string{
		problem.CategoryUsage:          "usage",
		problem.CategoryAuthPolicy:     "auth_policy",
		problem.CategoryModuleTrust:    "module_trust",
		problem.CategoryModuleProcess:  "module_process",
		problem.CategoryProductService: "product_service",
	}
	for category, want := range categories {
		if string(category) != want {
			t.Errorf("category %v has value %q, want %q", category, string(category), want)
		}
	}
}
