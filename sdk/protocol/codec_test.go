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

package protocol_test

import (
	"testing"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"github.com/wso2/wso2-cli/sdk/result"
)

func TestAResultSurvivesARoundTripInOrder(t *testing.T) {
	// Both sides of the contract share this translation, so a result the shell
	// decodes must be exactly the one the module encoded, fields and order
	// included.
	sent := result.New("reference.status/v1").
		With("organization", "Organization", "acme").
		With("service", "Service", "reference").
		With("checkedAt", "", "2026-07-27T09:30:00Z")

	got := protocol.DecodeResult(protocol.EncodeResult(sent))

	if got.Schema != sent.Schema {
		t.Errorf("schema round-tripped as %q, want %q", got.Schema, sent.Schema)
	}
	if len(got.Fields) != len(sent.Fields) {
		t.Fatalf("round trip produced %d fields, want %d", len(got.Fields), len(sent.Fields))
	}
	for index, field := range sent.Fields {
		if got.Fields[index] != field {
			t.Errorf("field %d round-tripped as %+v, want %+v", index, got.Fields[index], field)
		}
	}
}

func TestAProblemSurvivesARoundTrip(t *testing.T) {
	sent := problem.New(problem.CategoryProductService, "reference.status_unavailable",
		"the status service did not answer").WithRecovery("Try again shortly.")

	got, err := protocol.DecodeProblem(protocol.EncodeProblem(sent))
	if err != nil {
		t.Fatalf("decoding a well-formed problem: %v", err)
	}
	if got != sent {
		t.Errorf("problem round-tripped as %+v, want %+v", got, sent)
	}
}

func TestAnUnrecognizedProblemCategoryIsCarriedAsSent(t *testing.T) {
	// The receiver maps an unknown category to a process failure rather than
	// to success, so carrying it verbatim is safe and keeps the original
	// classification visible.
	got, err := protocol.DecodeProblem(&contractv1.Problem{
		Category: "a_category_from_the_future", Code: "probe.failed", Message: "it failed",
	})
	if err != nil {
		t.Fatalf("decoding a problem with an unknown category: %v", err)
	}
	if got.Category != problem.Category("a_category_from_the_future") {
		t.Errorf("category arrived as %q, want it carried as sent", got.Category)
	}
}

func TestAProblemTheReceiverCouldNotReportIsRejected(t *testing.T) {
	tests := map[string]*contractv1.Problem{
		"no code":    {Category: "usage", Message: "it failed"},
		"no message": {Category: "usage", Code: "probe.failed"},
	}
	for name, encoded := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := protocol.DecodeProblem(encoded); err == nil {
				t.Error("DecodeProblem accepted a failure that gives a user nothing to act on")
			}
		})
	}
}

func TestOutputModesSurviveARoundTrip(t *testing.T) {
	for _, mode := range []protocol.OutputMode{
		protocol.OutputModeUnspecified, protocol.OutputModeTable, protocol.OutputModeJSON,
	} {
		if got := protocol.DecodeOutputMode(protocol.EncodeOutputMode(mode)); got != mode {
			t.Errorf("%q round-tripped as %q", mode, got)
		}
	}
}

func TestAnOutputModeThisReleaseDoesNotKnowBecomesUnspecified(t *testing.T) {
	// The mode is advice. A receiver that cannot recognize it simply has no
	// advice to act on, which is not a reason to fail an invocation.
	if got := protocol.DecodeOutputMode(contractv1.OutputMode(999)); got != protocol.OutputModeUnspecified {
		t.Errorf("an unknown output mode decoded as %q, want unspecified", got)
	}
}

func TestEveryMessageKindIsNamedForDiagnostics(t *testing.T) {
	// An unexpected message is always reported as what it actually was, so
	// every kind the contract defines needs a name, including the one a future
	// release adds.
	kinds := map[string]*contractv1.Envelope{
		"a handshake":   {Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{}}},
		"a welcome":     {Message: &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{}}},
		"an invocation": {Message: &contractv1.Envelope_Invoke{Invoke: &contractv1.Invoke{}}},
		"a result":      {Message: &contractv1.Envelope_Result{Result: &contractv1.Result{}}},
		"a problem":     {Message: &contractv1.Envelope_Problem{Problem: &contractv1.Problem{}}},
		"a message this release does not recognize": {},
	}
	for want, envelope := range kinds {
		if got := protocol.DescribeMessage(envelope); got != want {
			t.Errorf("DescribeMessage reported %q, want %q", got, want)
		}
	}
}

func TestVersionsSurviveARoundTrip(t *testing.T) {
	if got := protocol.VersionsOf(&contractv1.Hello{
		ProtocolVersions: protocol.EncodeVersions([]int{3, 1}),
	}); len(got) != 2 || got[0] != 3 || got[1] != 1 {
		t.Errorf("versions round-tripped as %v, want [3 1]", got)
	}
}

func TestANonPositiveVersionIsDroppedRatherThanWrapped(t *testing.T) {
	// A version is a positive integer everywhere it is parsed. Encoding a
	// negative one as unsigned would advertise an enormous version number.
	if got := protocol.EncodeVersions([]int{1, 0, -2}); len(got) != 1 || got[0] != 1 {
		t.Errorf("EncodeVersions produced %v, want only the positive version", got)
	}
}

func TestVersionsAreFormattedNewestFirstForDiagnostics(t *testing.T) {
	if got := protocol.FormatVersions([]int{3, 1}); got != "v3, v1" {
		t.Errorf("FormatVersions reported %q, want %q", got, "v3, v1")
	}
	if got := protocol.FormatVersions(nil); got != "no version" {
		t.Errorf("FormatVersions reported %q for an empty list, want %q", got, "no version")
	}
}
