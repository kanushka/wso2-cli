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

package main

import (
	"context"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/testkit"
)

func TestResourceServersListReportsWhatEachOneIsIdentifiedBy(t *testing.T) {
	// The identifier is the whole point of the listing: it is the value an
	// operator copies into an account's product record as the audience, and
	// the exchange then asks for it as a resource indicator.
	stub := newThunderStub(t, map[string]string{
		"/resource-servers": `{"totalResults":1,"resourceServers":[
			{"id":"rs-1","name":"System","identifier":"https://localhost:8090/mcp","delimiter":":"}]}`,
	})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-servers", "list"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Err != nil || outcome.Problem != nil {
		t.Fatalf("resource-servers list failed: err=%v problem=%v", outcome.Err, outcome.Problem)
	}
	if !strings.Contains(renderFields(outcome), "https://localhost:8090/mcp") {
		t.Fatalf("the listing does not report the identifier:\n%s", renderFields(outcome))
	}
}

func TestCreatingAResourceServerRefusesAnIdentifierThatIsNotAnAbsoluteURI(t *testing.T) {
	// Measured against ThunderID 2026-09-09: a bare name comes back as
	// invalid_target only later, when a token exchange asks for it as an RFC
	// 8707 resource indicator — by which time the resource server exists and
	// the error names something else. Refusing here says the true cause once,
	// before anything is created.
	stub := newThunderStub(t, map[string]string{})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command:   []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "not-a-uri"},
		Context:   module.Context{Name: "c1", Endpoint: stub.URL},
		Access:    &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a non-URI identifier was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "absolute URI") {
		t.Fatalf("the refusal does not name the cause: %q", outcome.Problem.Message)
	}
}

func TestCreatingAResourceServerRefusesAPermissionCarryingTheDelimiter(t *testing.T) {
	// Also measured: ThunderID rejects a handle containing the resource
	// server's delimiter, and the refusal it returns names a delimiter the
	// caller never chose. Saying it here costs one comparison.
	stub := newThunderStub(t, map[string]string{})
	outcome := testkit.Run(context.Background(), moduleOptions(), commands().Commands(), testkit.Invocation{
		Command: []string{"resource-servers", "create"},
		Arguments: []string{"Hello API", "--identifier", "https://hello.example.test",
			"--permission", "hello:read"},
		Context: module.Context{Name: "c1", Endpoint: stub.URL},
		Access:  &testkit.Access{Token: "brokered-token"},
	})
	if outcome.Problem == nil {
		t.Fatalf("a permission carrying the delimiter was accepted: %+v", outcome.Result)
	}
	if !strings.Contains(outcome.Problem.Message, "delimiter") {
		t.Fatalf("the refusal does not name the cause: %q", outcome.Problem.Message)
	}
}

// renderFields flattens a result's values so a test can assert on what a table
// would show without depending on field order.
func renderFields(outcome testkit.Outcome) string {
	if outcome.Result == nil {
		return ""
	}
	var b strings.Builder
	for _, field := range outcome.Result.Fields {
		b.WriteString(field.Name + "=" + field.Value + "\n")
	}
	return b.String()
}
