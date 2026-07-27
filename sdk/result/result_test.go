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

package result_test

import (
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/result"
)

func TestFieldsKeepTheirDeclaredOrder(t *testing.T) {
	// The shell renders fields in this order in every output mode, so the
	// order a module declares is part of the result.
	built := result.New("reference.status/v1").
		With("organization", "Organization", "acme").
		With("service", "Service", "reference").
		With("status", "Status", "operational")

	var names []string
	for _, field := range built.Fields {
		names = append(names, field.Name)
	}
	if got, want := strings.Join(names, ","), "organization,service,status"; got != want {
		t.Errorf("field order is %q, want %q", got, want)
	}
}

func TestWithDoesNotMutateTheReceiver(t *testing.T) {
	// A handler may build several results from a shared base, so appending a
	// field must not reach back into the value it was derived from.
	base := result.New("reference.status/v1").With("organization", "Organization", "acme")

	first := base.With("status", "Status", "operational")
	second := base.With("status", "Status", "degraded")

	if len(base.Fields) != 1 {
		t.Fatalf("the base result grew to %d fields; With must copy", len(base.Fields))
	}
	if first.Fields[1].Value == second.Fields[1].Value {
		t.Fatal("two results derived from one base share a field value")
	}
}

func TestLabelFallsBackToTheFieldName(t *testing.T) {
	built := result.New("reference.status/v1").With("checkedAt", "", "2026-07-27T00:00:00Z")

	if got := built.Fields[0].DisplayLabel(); got != "checkedAt" {
		t.Errorf("a field with no label displays as %q, want its name", got)
	}
}

func TestValidateRejectsResultsTheShellCouldNotRender(t *testing.T) {
	tests := map[string]result.Result{
		"no schema": {Fields: []result.Field{{Name: "status", Value: "operational"}}},
		"no fields": result.New("reference.status/v1"),
		"unnamed field": {
			Schema: "reference.status/v1",
			Fields: []result.Field{{Name: "", Value: "operational"}},
		},
		"duplicate field name": {
			Schema: "reference.status/v1",
			Fields: []result.Field{{Name: "status", Value: "operational"}, {Name: "status", Value: "degraded"}},
		},
	}

	for name, invalid := range tests {
		t.Run(name, func(t *testing.T) {
			if err := invalid.Validate(); err == nil {
				t.Error("Validate accepted a result the shell could not render")
			}
		})
	}
}

func TestValidateAcceptsAWellFormedResult(t *testing.T) {
	built := result.New("reference.status/v1").
		With("organization", "Organization", "acme").
		With("checkedAt", "Checked at", "2026-07-27T00:00:00Z")

	if err := built.Validate(); err != nil {
		t.Errorf("Validate rejected a well-formed result: %v", err)
	}
}
