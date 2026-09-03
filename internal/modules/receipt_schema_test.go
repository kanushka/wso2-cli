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

package modules_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/commandtree"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// validReceipt is a receipt that passes validation, so a test can change one
// field and know the change is what the assertion is about.
func validReceipt() modules.Receipt {
	return modules.Receipt{
		SchemaVersion: modules.ReceiptSchemaVersion,
		Namespace:     "reference",
		ModuleVersion: "1.2.3",
		Executable:    "wso2-module-reference",
		Compatibility: modules.Compatibility{
			Shell:            ">=0.1.0 <1.0.0",
			ProtocolVersions: []int{2},
		},
		Platform:         modules.Platform{OS: "darwin", Arch: "arm64"},
		ExecutableSHA256: strings.Repeat("a", 64),
	}
}

// TestAReceiptWrittenBeforeCommandTreesIsRefused pins the cost of the schema
// bump, so that it is a decision on the record rather than a surprise in the
// field.
//
// A version 1 receipt carries no declared command tree, and the tree is what the
// shell now parses a product command line with. Reading such a receipt anyway
// would leave that module parsing by one rule while every other module parsed by
// another. It is refused instead, and the refusal names the way out.
func TestAReceiptWrittenBeforeCommandTreesIsRefused(t *testing.T) {
	old := validReceipt()
	old.SchemaVersion = 1

	err := old.Validate()

	var reported problem.Problem
	if !errors.As(err, &reported) {
		t.Fatalf("validating a version 1 receipt returned %v, want a typed problem", err)
	}
	if reported.Code != "modules.receipt_schema_unsupported" {
		t.Errorf("the refusal is coded %q", reported.Code)
	}
	if !strings.Contains(strings.ToLower(reported.Recovery), "reinstall") {
		t.Errorf("the refusal recovers with %q, which does not tell the user to reinstall", reported.Recovery)
	}
}

// TestTheCurrentSchemaIsTheOneCommandTreesArrivedIn guards the constant against
// being moved without the decision being revisited. Every installed receipt is
// written with this value, so changing it refuses every installation until each
// module is installed again.
func TestTheCurrentSchemaIsTheOneCommandTreesArrivedIn(t *testing.T) {
	if modules.ReceiptSchemaVersion != 2 {
		t.Errorf("the receipt schema version is %d; moving it invalidates every installed receipt",
			modules.ReceiptSchemaVersion)
	}
}

// TestAReceiptCarriesTheDeclaredTreeThroughEncoding proves the tree survives the
// round trip to disk. The receipt is the only source the shell parses from, so a
// tree that encoded but did not decode would silently return every module to
// positional passthrough.
func TestAReceiptCarriesTheDeclaredTreeThroughEncoding(t *testing.T) {
	written := validReceipt()
	written.CommandTree = commandtree.New([]commandtree.Command{
		{Path: []string{"status"}, Runnable: true, Flags: []commandtree.Flag{
			{Name: "since", Type: "string"},
		}},
	})

	encoded, err := written.Encode()
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}
	read, err := modules.DecodeReceipt(encoded)
	if err != nil {
		t.Fatalf("decoding: %v", err)
	}

	command, ok := read.CommandTree.Child(nil, "status")
	if !ok {
		t.Fatalf("the decoded receipt declares %+v", read.CommandTree)
	}
	if flag, found := command.LookupFlag("since"); !found || !flag.TakesValue() {
		t.Errorf("the decoded flag is %+v, found %v", flag, found)
	}
}

// TestAReceiptWithNoDeclaredTreeIsValid proves the empty tree is a supported
// state. A module that does not use Cobra declares nothing, and refusing it
// would make declaring mandatory for everyone.
func TestAReceiptWithNoDeclaredTreeIsValid(t *testing.T) {
	if err := validReceipt().Validate(); err != nil {
		t.Errorf("a receipt with no declared tree is invalid: %v", err)
	}
}
