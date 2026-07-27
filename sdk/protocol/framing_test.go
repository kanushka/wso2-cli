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
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/sdk/protocol"
	contractv1 "github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"google.golang.org/protobuf/proto"
)

func TestFramesRoundTripInOrder(t *testing.T) {
	sent := []*contractv1.Envelope{
		{Message: &contractv1.Envelope_Hello{Hello: &contractv1.Hello{
			Module:           &contractv1.ModuleIdentity{Namespace: "reference", Version: "0.1.0"},
			ProtocolVersions: []uint32{1},
		}}},
		{InvocationId: "inv-1", Message: &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{ProtocolVersion: 1}}},
		{InvocationId: "inv-1", Message: &contractv1.Envelope_Result{Result: &contractv1.Result{Schema: "reference.status/v1"}}},
	}

	var wire bytes.Buffer
	writer := protocol.NewWriter(&wire)
	for _, envelope := range sent {
		if err := writer.WriteEnvelope(envelope); err != nil {
			t.Fatalf("writing envelope: %v", err)
		}
	}

	reader := protocol.NewReader(&wire)
	for index, want := range sent {
		got, err := reader.ReadEnvelope()
		if err != nil {
			t.Fatalf("reading envelope %d: %v", index, err)
		}
		if !proto.Equal(got, want) {
			t.Errorf("envelope %d round-tripped as %v, want %v", index, got, want)
		}
	}
	if _, err := reader.ReadEnvelope(); !errors.Is(err, io.EOF) {
		t.Errorf("after the last frame the reader reported %v, want io.EOF", err)
	}
}

func TestAnEmptyStreamEndsCleanly(t *testing.T) {
	// A module that exits without writing anything must be distinguishable
	// from one that wrote a damaged frame, so the shell can report a missing
	// terminal message rather than a decoding failure.
	_, err := protocol.NewReader(strings.NewReader("")).ReadEnvelope()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("an empty stream reported %v, want io.EOF", err)
	}
}

func TestATruncatedPayloadIsRejected(t *testing.T) {
	var wire bytes.Buffer
	if err := protocol.NewWriter(&wire).WriteEnvelope(&contractv1.Envelope{InvocationId: "inv-1"}); err != nil {
		t.Fatalf("writing envelope: %v", err)
	}
	truncated := wire.Bytes()[:wire.Len()-1]

	_, err := protocol.NewReader(bytes.NewReader(truncated)).ReadEnvelope()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a truncated frame reported %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestATruncatedLengthPrefixIsRejected(t *testing.T) {
	// 0x80 continues the varint, so the length itself is incomplete.
	_, err := protocol.NewReader(bytes.NewReader([]byte{0x80})).ReadEnvelope()
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("a truncated length prefix reported %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestAnOversizedFrameIsRejectedWithoutReadingIt(t *testing.T) {
	// The declared length is honoured before the payload is read, so a hostile
	// length cannot make the shell allocate or consume the stated size.
	var length [binary.MaxVarintLen64]byte
	written := binary.PutUvarint(length[:], uint64(protocol.MaxFrameBytes)+1)

	source := &countingReader{Reader: bytes.NewReader(length[:written])}
	_, err := protocol.NewReader(source).ReadEnvelope()
	if !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("an oversized frame reported %v, want protocol.ErrFrameTooLarge", err)
	}
	if source.bytesRead > written {
		t.Errorf("the reader consumed %d bytes for a rejected frame, want at most the %d-byte length prefix",
			source.bytesRead, written)
	}
}

func TestAFrameAtTheSizeLimitIsAccepted(t *testing.T) {
	// The limit is inclusive, so a message of exactly the maximum size is a
	// valid frame rather than a boundary failure.
	large := &contractv1.Envelope{Message: &contractv1.Envelope_Problem{Problem: &contractv1.Problem{
		Message: strings.Repeat("d", protocol.MaxFrameBytes-16),
	}}}
	encoded, err := proto.Marshal(large)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	if len(encoded) > protocol.MaxFrameBytes {
		t.Fatalf("the fixture is %d bytes, which exceeds the %d-byte limit", len(encoded), protocol.MaxFrameBytes)
	}

	var wire bytes.Buffer
	if err := protocol.NewWriter(&wire).WriteEnvelope(large); err != nil {
		t.Fatalf("writing a maximum-size envelope: %v", err)
	}
	if _, err := protocol.NewReader(&wire).ReadEnvelope(); err != nil {
		t.Fatalf("reading a maximum-size envelope: %v", err)
	}
}

func TestWritingAnOversizedEnvelopeFails(t *testing.T) {
	// A peer must not be able to emit a frame it would itself reject.
	oversized := &contractv1.Envelope{Message: &contractv1.Envelope_Problem{Problem: &contractv1.Problem{
		Message: strings.Repeat("d", protocol.MaxFrameBytes+1),
	}}}

	var wire bytes.Buffer
	err := protocol.NewWriter(&wire).WriteEnvelope(oversized)
	if !errors.Is(err, protocol.ErrFrameTooLarge) {
		t.Fatalf("writing an oversized envelope reported %v, want protocol.ErrFrameTooLarge", err)
	}
	if wire.Len() != 0 {
		t.Errorf("a rejected write emitted %d bytes; it must emit none", wire.Len())
	}
}

func TestAMalformedPayloadIsRejected(t *testing.T) {
	// A well-framed but undecodable payload must not be confused with a
	// transport failure.
	payload := []byte{0xff, 0xff, 0xff, 0xff}
	var wire bytes.Buffer
	var length [binary.MaxVarintLen64]byte
	wire.Write(length[:binary.PutUvarint(length[:], uint64(len(payload)))])
	wire.Write(payload)

	_, err := protocol.NewReader(&wire).ReadEnvelope()
	if !errors.Is(err, protocol.ErrMalformedFrame) {
		t.Fatalf("a malformed payload reported %v, want protocol.ErrMalformedFrame", err)
	}
}

func TestUnknownFieldsAreToleratedForAdditiveCompatibility(t *testing.T) {
	// A newer peer may add a field within this protocol version. An older
	// reader must keep the fields it knows rather than failing closed.
	known := &contractv1.Envelope{
		InvocationId: "inv-1",
		Message:      &contractv1.Envelope_Result{Result: &contractv1.Result{Schema: "reference.status/v1"}},
	}
	encoded, err := proto.Marshal(known)
	if err != nil {
		t.Fatalf("marshalling: %v", err)
	}
	// Field 4095, wire type 2 (length-delimited), carrying four bytes.
	future := append(encoded, 0xfa, 0xff, 0x01, 0x04, 'n', 'e', 'x', 't')

	var wire bytes.Buffer
	var length [binary.MaxVarintLen64]byte
	wire.Write(length[:binary.PutUvarint(length[:], uint64(len(future)))])
	wire.Write(future)

	got, err := protocol.NewReader(&wire).ReadEnvelope()
	if err != nil {
		t.Fatalf("reading an envelope with an unknown field: %v", err)
	}
	if got.GetInvocationId() != "inv-1" {
		t.Errorf("invocation ID is %q, want %q", got.GetInvocationId(), "inv-1")
	}
	if got.GetResult().GetSchema() != "reference.status/v1" {
		t.Errorf("result schema is %q, want %q", got.GetResult().GetSchema(), "reference.status/v1")
	}
}

// countingReader records how many bytes a reader consumed from its source.
type countingReader struct {
	io.Reader
	bytesRead int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.bytesRead += n
	return n, err
}
