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

package protocol

import (
	"errors"
	"strconv"
	"strings"

	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"github.com/wso2/wso2-cli/sdk/result"
)

// This file is the one place the contract's wire types are translated to and
// from the Go types the shell and a module actually work with.
//
// Both sides of the contract need the same translation, and a shell that
// decoded a result differently from the module that encoded it would be a
// silent compatibility break. Keeping the pair here means they cannot drift.

// OutputMode is the rendering the shell will apply to a result.
//
// A module is told which one is in force, but only as advice: it may use it to
// skip work whose only purpose is display, never to format or write user output
// itself. See docs/adr/0003-shell-owned-output.md.
type OutputMode string

const (
	// OutputModeUnspecified means no mode was stated.
	OutputModeUnspecified OutputMode = ""
	// OutputModeTable means a human-readable table.
	OutputModeTable OutputMode = "table"
	// OutputModeJSON means deterministic JSON.
	OutputModeJSON OutputMode = "json"
)

// EncodeOutputMode renders an output mode on the wire.
func EncodeOutputMode(mode OutputMode) contractv1.OutputMode {
	switch mode {
	case OutputModeTable:
		return contractv1.OutputMode_OUTPUT_MODE_TABLE
	case OutputModeJSON:
		return contractv1.OutputMode_OUTPUT_MODE_JSON
	default:
		return contractv1.OutputMode_OUTPUT_MODE_UNSPECIFIED
	}
}

// DecodeOutputMode reads an output mode from the wire. A mode this release does
// not know is reported as unspecified, because the mode is advice and a module
// that cannot recognize it simply has no advice to act on.
func DecodeOutputMode(mode contractv1.OutputMode) OutputMode {
	switch mode {
	case contractv1.OutputMode_OUTPUT_MODE_TABLE:
		return OutputModeTable
	case contractv1.OutputMode_OUTPUT_MODE_JSON:
		return OutputModeJSON
	default:
		return OutputModeUnspecified
	}
}

// EncodeResult renders a command result on the wire, preserving field order.
func EncodeResult(produced result.Result) *contractv1.Result {
	fields := make([]*contractv1.ResultField, 0, len(produced.Fields))
	for _, field := range produced.Fields {
		fields = append(fields, &contractv1.ResultField{
			Name:  field.Name,
			Label: field.Label,
			Value: field.Value,
		})
	}
	return &contractv1.Result{Schema: produced.Schema, Fields: fields}
}

// DecodeResult reads a command result from the wire.
//
// It does not validate: a decoded result is a peer's claim, and the caller
// decides what an unrenderable one means. Call result.Result.Validate on the
// value it returns.
func DecodeResult(encoded *contractv1.Result) result.Result {
	decoded := result.Result{Schema: encoded.GetSchema()}
	for _, field := range encoded.GetFields() {
		decoded.Fields = append(decoded.Fields, result.Field{
			Name:  field.GetName(),
			Label: field.GetLabel(),
			Value: field.GetValue(),
		})
	}
	return decoded
}

// EncodeProblem renders a typed failure on the wire.
func EncodeProblem(failure problem.Problem) *contractv1.Problem {
	return &contractv1.Problem{
		Category: string(failure.Category),
		Code:     failure.Code,
		Message:  failure.Message,
		Recovery: failure.Recovery,
	}
}

// DecodeProblem reads a typed failure from the wire.
//
// A failure with no code or no message is rejected, because it gives a user
// nothing to act on and nothing to search for. The category is carried as sent
// even when this release does not recognize it: the receiver maps an unknown
// category to a process failure rather than to success, so an unknown class can
// never be mistaken for a working command.
func DecodeProblem(encoded *contractv1.Problem) (problem.Problem, error) {
	if encoded.GetCode() == "" {
		return problem.Problem{}, errors.New("it carries no problem code")
	}
	if encoded.GetMessage() == "" {
		return problem.Problem{}, errors.New("it carries no message")
	}
	return problem.Problem{
		Category: problem.Category(encoded.GetCategory()),
		Code:     encoded.GetCode(),
		Message:  encoded.GetMessage(),
		Recovery: encoded.GetRecovery(),
	}, nil
}

// DescribeMessage names the payload an envelope carried, so a diagnostic can
// say what arrived instead of only what was expected.
//
// An envelope kind this release does not know is named rather than skipped,
// because a skipped message may have been the one that mattered.
func DescribeMessage(envelope *contractv1.Envelope) string {
	switch envelope.GetMessage().(type) {
	case *contractv1.Envelope_Hello:
		return "a handshake"
	case *contractv1.Envelope_Welcome:
		return "a welcome"
	case *contractv1.Envelope_Invoke:
		return "an invocation"
	case *contractv1.Envelope_AcquireAccess:
		return "an access request"
	case *contractv1.Envelope_AccessGranted:
		return "an access grant"
	case *contractv1.Envelope_AccessDenied:
		return "an access denial"
	case *contractv1.Envelope_Result:
		return "a result"
	case *contractv1.Envelope_Problem:
		return "a problem"
	default:
		return "a message this release does not recognize"
	}
}

// FormatVersions renders protocol versions for a diagnostic, newest first.
func FormatVersions(versions []int) string {
	if len(versions) == 0 {
		return "no version"
	}
	rendered := make([]string, 0, len(versions))
	for _, version := range versions {
		rendered = append(rendered, "v"+strconv.Itoa(version))
	}
	return strings.Join(rendered, ", ")
}

// VersionsOf reads the protocol versions an envelope's handshake offered.
func VersionsOf(hello *contractv1.Hello) []int {
	versions := make([]int, 0, len(hello.GetProtocolVersions()))
	for _, version := range hello.GetProtocolVersions() {
		versions = append(versions, int(version))
	}
	return versions
}

// EncodeVersions renders protocol versions on the wire. A version is a positive
// integer everywhere it is parsed, so a non-positive one is dropped rather than
// wrapped into a large unsigned value.
func EncodeVersions(versions []int) []uint32 {
	encoded := make([]uint32, 0, len(versions))
	for _, version := range versions {
		if version <= 0 {
			continue
		}
		encoded = append(encoded, uint32(version))
	}
	return encoded
}
