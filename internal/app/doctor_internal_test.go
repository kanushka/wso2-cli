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
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// The four synthetic problems below stand in for what each check actually
// returns, distinguished by Code rather than by Category: secure-store and
// session both carry problem.CategoryAuthPolicy, so a test that only checked
// the category, or the exit class exit.ForProblem derives from it, could not
// tell those two apart and would pass against a wrong answer.
var (
	syntheticContextFailure = problem.New(problem.CategoryUsage,
		"contexts.document_malformed", "synthetic context failure")
	syntheticSecureStoreFailure = problem.New(problem.CategoryAuthPolicy,
		"auth.keyring_unavailable", "synthetic secure-store failure")
	syntheticSessionFailure = problem.New(problem.CategoryAuthPolicy,
		"auth.login_required", "synthetic session failure")
	syntheticCatalogFailure = problem.New(problem.CategoryModuleProcess,
		"catalog.origin_unreachable", "synthetic catalog failure")
)

// TestMostSevereFailure pins every position of severityRank directly, by
// code, rather than through shell.Run: after doctor.go's session check became
// not-applicable whenever the document fails to load (see
// TestDoctorRanksTheDocumentAboveAnAbsentSession), context and session can no
// longer both fail in the same real invocation, so that pair of the rank is
// unreachable end to end and needs a direct test of mostSevereFailure itself.
// internal/app already carries in-package test files (invoke_test.go,
// outputflag_internal_test.go), so this needs no export seam.
func TestMostSevereFailure(t *testing.T) {
	for name, testCase := range map[string]struct {
		failures map[string]problem.Problem
		want     *problem.Problem
	}{
		"nothing failed": {
			failures: map[string]problem.Problem{},
			want:     nil,
		},
		"context alone": {
			failures: map[string]problem.Problem{checkContext: syntheticContextFailure},
			want:     &syntheticContextFailure,
		},
		"secure-store alone": {
			failures: map[string]problem.Problem{checkSecureStore: syntheticSecureStoreFailure},
			want:     &syntheticSecureStoreFailure,
		},
		"session alone": {
			failures: map[string]problem.Problem{checkSession: syntheticSessionFailure},
			want:     &syntheticSessionFailure,
		},
		"catalog alone": {
			failures: map[string]problem.Problem{checkCatalog: syntheticCatalogFailure},
			want:     &syntheticCatalogFailure,
		},
		"context and session: context is unreachable through shell.Run and needs this test": {
			failures: map[string]problem.Problem{
				checkContext: syntheticContextFailure,
				checkSession: syntheticSessionFailure,
			},
			want: &syntheticContextFailure,
		},
		"secure-store and context: secure-store outranks the document": {
			failures: map[string]problem.Problem{
				checkSecureStore: syntheticSecureStoreFailure,
				checkContext:     syntheticContextFailure,
			},
			want: &syntheticSecureStoreFailure,
		},
		"secure-store and session: both share exit.AuthPolicy, only the code tells them apart": {
			failures: map[string]problem.Problem{
				checkSecureStore: syntheticSecureStoreFailure,
				checkSession:     syntheticSessionFailure,
			},
			want: &syntheticSecureStoreFailure,
		},
		"context and catalog: the document outranks catalog": {
			failures: map[string]problem.Problem{
				checkContext: syntheticContextFailure,
				checkCatalog: syntheticCatalogFailure,
			},
			want: &syntheticContextFailure,
		},
		"session and catalog: session outranks catalog": {
			failures: map[string]problem.Problem{
				checkSession: syntheticSessionFailure,
				checkCatalog: syntheticCatalogFailure,
			},
			want: &syntheticSessionFailure,
		},
		"all four: secure-store wins over everything": {
			failures: map[string]problem.Problem{
				checkContext:     syntheticContextFailure,
				checkSecureStore: syntheticSecureStoreFailure,
				checkSession:     syntheticSessionFailure,
				checkCatalog:     syntheticCatalogFailure,
			},
			want: &syntheticSecureStoreFailure,
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := mostSevereFailure(testCase.failures)
			if testCase.want == nil {
				if err != nil {
					t.Fatalf("mostSevereFailure = %v, want nil", err)
				}
				return
			}
			var typed problem.Problem
			if !errors.As(err, &typed) {
				t.Fatalf("mostSevereFailure returned an untyped error: %v", err)
			}
			if typed.Code != testCase.want.Code {
				t.Errorf("mostSevereFailure code = %q, want %q", typed.Code, testCase.want.Code)
			}
		})
	}
}
