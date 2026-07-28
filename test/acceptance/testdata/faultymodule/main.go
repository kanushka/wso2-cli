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

// Command faultymodule speaks the module contract by hand so the shell's
// acceptance tests can drive one fault at a time.
//
// The SDK exists to make a module conform, so a module built on it cannot lie
// about its identity, require a capability the shell has never heard of, or put
// a damaged frame on the wire. Those are exactly the failures the shell has to
// survive, so this module writes its own frames instead. It is fixture code and
// never ships: nothing in the shell, the SDK, or the reference module depends
// on it.
//
// It claims the reference namespace and version so the shell resolves and
// launches it exactly as it would the reference module. Which fault it injects
// comes from a control file named "fault" beside its own executable, because
// the shell passes a module no arguments and sanitizes its environment to
// nothing. Writing that file also leaves the executable's bytes unchanged, so
// its receipt digest still matches and the shell still launches it.
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"google.golang.org/protobuf/proto"
)

// The faults this module can inject. Each name is also the content of the
// control file that selects it.
const (
	// FaultNone answers correctly. It is the control case that proves a
	// hand-written module reaches the same success as the reference module,
	// so a failing assertion elsewhere is about the injected fault.
	FaultNone = ""
	// FaultNamespaceMismatch reports a namespace the receipt does not name.
	FaultNamespaceMismatch = "namespace-mismatch"
	// FaultVersionMismatch reports a version the receipt does not name.
	FaultVersionMismatch = "version-mismatch"
	// FaultRequiredCapability requires a protocol capability no shell provides.
	FaultRequiredCapability = "required-capability"
	// FaultRuntimeProtocol offers a protocol version the receipt did not
	// promise, after resolution already selected another.
	FaultRuntimeProtocol = "runtime-protocol"
	// FaultUnknownMessageKind sends an envelope whose payload is a message kind
	// this protocol release does not define.
	FaultUnknownMessageKind = "unknown-message-kind"
	// FaultUnknownField sends a correct result carrying one field this protocol
	// release does not define. It is the fault that must not fail: additive
	// compatibility means an unknown field is ignored, not refused.
	FaultUnknownField = "unknown-field"
	// FaultWaitForInputClose answers nothing and exits only once the shell
	// closes its protocol input.
	FaultWaitForInputClose = "wait-for-input-close"
	// FaultTruncatedFrame declares a frame longer than the bytes that follow.
	FaultTruncatedFrame = "truncated-frame"
	// FaultPartialLengthPrefix stops part-way through a frame's length prefix.
	FaultPartialLengthPrefix = "partial-length-prefix"
	// FaultMalformedFrame sends a complete frame that is not a decodable
	// envelope.
	FaultMalformedFrame = "malformed-frame"
	// FaultOversizedFrame declares a frame larger than the shell will read.
	FaultOversizedFrame = "oversized-frame"
	// FaultExtraFrame keeps writing frames after its terminal result.
	FaultExtraFrame = "extra-frame"
	// FaultPanic panics after the handshake, without a terminal message.
	FaultPanic = "panic"
	// FaultFloodDiagnostics writes far more standard error than the shell keeps,
	// then answers correctly.
	FaultFloodDiagnostics = "flood-diagnostics"
)

// ControlFile is the name of the file, written beside the installed executable,
// whose content selects the fault.
const ControlFile = "fault"

// Markers this module leaves beside its own executable, so a test can assert
// how far the exchange actually got rather than infer it from the shell's own
// account of the failure.
const (
	// InvokedMarker records that the shell sent a command invocation. A
	// handshake the shell refused never reaches it.
	InvokedMarker = "invocation-arrived"
	// InputClosedMarker records that this module exited because the shell
	// closed its protocol input, rather than because it was killed.
	InputClosedMarker = "input-was-closed"
)

// FloodBytes is how much standard error FaultFloodDiagnostics writes. It is far
// above any plausible shell limit, so the test does not depend on the exact one.
const FloodBytes = 512 << 10

// CapabilityName is the capability FaultRequiredCapability demands.
const CapabilityName = "reference.streaming"

// ImpostorNamespace and ImpostorVersion are the identity the mismatch faults
// report. Neither can match a receipt this module is installed under.
const (
	ImpostorNamespace = "impostor"
	ImpostorVersion   = "0.0.1-impostor"
)

// moduleVersion is injected by the acceptance test so this module matches the
// receipt it is installed under.
var moduleVersion = "0.0.0-dev"

func main() {
	if err := run(selectedFault()); err != nil {
		fmt.Fprintf(os.Stderr, "faultymodule: %v\n", err)
		os.Exit(1)
	}
}

func run(fault string) error {
	reader := protocol.NewReader(os.Stdin)
	writer := protocol.NewWriter(os.Stdout)

	if err := sendHello(writer, fault); err != nil {
		return err
	}

	// A shell that refuses the handshake closes this module's protocol input
	// rather than answering, so the read below ends and the module exits
	// cleanly. Only a shell that accepted the handshake reaches a fault below.
	invocationID, accepted, err := readHandshakeReply(reader)
	if err != nil {
		return err
	}
	if !accepted {
		return nil
	}

	switch fault {
	case FaultPanic:
		panic("the faulty module panicked instead of answering")
	case FaultFloodDiagnostics:
		flood()
	case FaultWaitForInputClose:
		// Nothing more will arrive, so this read ends only when the shell gives
		// up and closes the stream. A module killed outright never gets here.
		if _, err := reader.ReadEnvelope(); !streamEnded(err) {
			return fmt.Errorf("the shell did not close the protocol input: %w", err)
		}
		return mark(InputClosedMarker)
	}

	if raw, damaged := damagedFrame(fault, invocationID); damaged {
		_, err := os.Stdout.Write(raw)
		return err
	}

	if fault == FaultUnknownField {
		_, err := os.Stdout.Write(frame(withUnknownField(statusResult(invocationID))))
		return err
	}
	if err := writer.WriteEnvelope(statusResult(invocationID)); err != nil {
		return err
	}
	if fault == FaultExtraFrame {
		// A second terminal message means this module and the shell disagree
		// about where the invocation ended.
		return writer.WriteEnvelope(statusResult(invocationID))
	}
	return nil
}

// sendHello opens the handshake, stating an identity or capability set the
// selected fault has corrupted.
func sendHello(writer *protocol.Writer, fault string) error {
	identity := &contractv1.ModuleIdentity{Namespace: "reference", Version: moduleVersion}
	hello := &contractv1.Hello{
		Module:           identity,
		ProtocolVersions: protocol.EncodeVersions(protocol.Supported()),
	}
	switch fault {
	case FaultNamespaceMismatch:
		identity.Namespace = ImpostorNamespace
	case FaultVersionMismatch:
		identity.Version = ImpostorVersion
	case FaultRequiredCapability:
		hello.RequiredCapabilities = []string{CapabilityName}
	case FaultRuntimeProtocol:
		// Every version this module actually speaks is withheld, so the one
		// the receipt promised and the shell selected is not on offer.
		hello.ProtocolVersions = nil
		for _, version := range protocol.Supported() {
			hello.ProtocolVersions = append(hello.ProtocolVersions, uint32(version)+1)
		}
	}
	return writer.WriteEnvelope(&contractv1.Envelope{Message: &contractv1.Envelope_Hello{Hello: hello}})
}

// readHandshakeReply reads the shell's welcome and invocation, and reports
// whether the shell accepted the handshake at all.
func readHandshakeReply(reader *protocol.Reader) (invocationID string, accepted bool, err error) {
	welcome, err := reader.ReadEnvelope()
	if streamEnded(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading the shell's welcome: %w", err)
	}
	if welcome.GetWelcome() == nil {
		return "", false, fmt.Errorf("expected a welcome, got %s", protocol.DescribeMessage(welcome))
	}

	invoke, err := reader.ReadEnvelope()
	if streamEnded(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("reading the invocation: %w", err)
	}
	if invoke.GetInvoke() == nil {
		return "", false, fmt.Errorf("expected an invocation, got %s", protocol.DescribeMessage(invoke))
	}
	if err := mark(InvokedMarker); err != nil {
		return "", false, err
	}
	return welcome.GetInvocationId(), true, nil
}

// mark records that the exchange reached a point, beside this executable.
func mark(name string) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locating this executable: %w", err)
	}
	return os.WriteFile(filepath.Join(filepath.Dir(executable), name), nil, 0o644)
}

// streamEnded reports a protocol input the shell closed, whether it closed
// between frames or part-way through one. Either way there is no shell left to
// answer, and this module is done.
func streamEnded(err error) bool {
	return errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)
}

// damagedFrame returns the raw bytes of a frame the shell must refuse, and
// whether the selected fault produces one. These are written to standard output
// directly, because a conforming writer cannot emit them.
func damagedFrame(fault, invocationID string) ([]byte, bool) {
	switch fault {
	case FaultUnknownMessageKind:
		return frame(unknownMessageKindEnvelope(invocationID)), true
	case FaultTruncatedFrame:
		// A frame that promises a hundred bytes and delivers ten.
		payload := frame(make([]byte, 100))
		return payload[:len(payload)-90], true
	case FaultPartialLengthPrefix:
		// A varint byte whose continuation bit promises another that never
		// arrives.
		return []byte{0x80}, true
	case FaultMalformedFrame:
		// A complete frame whose payload cannot be a protobuf message: field
		// number zero is invalid in every encoding.
		return frame([]byte{0x00, 0x01, 0x02}), true
	case FaultOversizedFrame:
		// Only the length prefix is written. A shell that allocated or read
		// before checking it would be the fault this proves absent.
		var prefix [binary.MaxVarintLen64]byte
		return prefix[:binary.PutUvarint(prefix[:], protocol.MaxFrameBytes+1)], true
	default:
		return nil, false
	}
}

// unknownMessageKindEnvelope encodes an envelope carrying a payload from a
// protocol release this one does not know.
//
// A future message kind arrives as an unknown field of the envelope's payload
// oneof, so it is written here as a field number this release has not assigned.
// Decoding must succeed and still leave the shell with no message it can act on.
func unknownMessageKindEnvelope(invocationID string) []byte {
	return appendUnassignedField(encode(&contractv1.Envelope{InvocationId: invocationID}))
}

// withUnknownField encodes a well-formed envelope and adds one field this
// release has not assigned.
//
// It is the additive half of the same wire fact: the envelope still carries a
// message the shell knows, so the unknown field must be ignored rather than
// treated as the unknown kind above.
func withUnknownField(envelope *contractv1.Envelope) []byte {
	return appendUnassignedField(encode(envelope))
}

// appendUnassignedField adds a length-delimited field whose number this
// protocol release has not assigned. A later release would use it for a message
// kind or a new envelope field; both look like this on the wire.
func appendUnassignedField(encoded []byte) []byte {
	const unassignedFieldNumber = 18
	const lengthDelimited = 2
	var tag [binary.MaxVarintLen64]byte
	written := binary.PutUvarint(tag[:], uint64(unassignedFieldNumber)<<3|lengthDelimited)
	encoded = append(encoded, tag[:written]...)
	// An empty payload: what the unknown field contains is not what the shell
	// has to survive, its being unrecognized is.
	return append(encoded, 0x00)
}

func encode(envelope *contractv1.Envelope) []byte {
	encoded, err := proto.Marshal(envelope)
	if err != nil {
		panic("faultymodule: cannot encode an envelope: " + err.Error())
	}
	return encoded
}

// frame prefixes a payload with its unsigned-varint length.
func frame(payload []byte) []byte {
	var prefix [binary.MaxVarintLen64]byte
	written := binary.PutUvarint(prefix[:], uint64(len(payload)))
	return append(prefix[:written], payload...)
}

// statusResult is the terminal message a conforming reference status returns.
func statusResult(invocationID string) *contractv1.Envelope {
	return &contractv1.Envelope{
		InvocationId: invocationID,
		Message: &contractv1.Envelope_Result{Result: &contractv1.Result{
			Schema: "reference.status/v1",
			Fields: []*contractv1.ResultField{
				{Name: "organization", Label: "Organization", Value: "reference-org"},
				{Name: "service", Label: "Service", Value: "reference"},
				{Name: "status", Label: "Status", Value: "operational"},
				{Name: "checkedAt", Label: "Checked at", Value: time.Now().UTC().Format(time.RFC3339)},
			},
		}},
	}
}

// flood writes more diagnostics than any shell should keep.
func flood() {
	line := strings.Repeat("d", 63) + "\n"
	for written := 0; written < FloodBytes; written += len(line) {
		fmt.Fprint(os.Stderr, line)
	}
}

// selectedFault reads the control file beside this executable. An absent or
// unreadable file selects no fault, so a mis-installed fixture answers correctly
// and fails its test loudly rather than passing for the wrong reason.
func selectedFault() string {
	executable, err := os.Executable()
	if err != nil {
		return FaultNone
	}
	content, err := os.ReadFile(filepath.Join(filepath.Dir(executable), ControlFile))
	if err != nil {
		return FaultNone
	}
	return strings.TrimSpace(string(content))
}
