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
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"sync"

	contractv1 "github.com/wso2/wso2-cli/sdk/protocol/contractv1"
	"google.golang.org/protobuf/proto"
)

// MaxFrameBytes is the largest encoded envelope either side will read or write.
//
// It bounds what a peer can make its counterpart allocate. The value is an
// internal constant of the architecture proof covered by tests, not yet a
// public compatibility promise: a module must not depend on being able to send
// a frame of exactly this size.
const MaxFrameBytes = 4 << 20

// Framing failures. They are distinguished so a caller can report a damaged
// stream separately from a peer that simply stopped writing.
var (
	// ErrFrameTooLarge reports a frame whose declared length exceeds
	// MaxFrameBytes. The payload is never read.
	ErrFrameTooLarge = errors.New("protocol: frame exceeds the maximum frame size")
	// ErrMalformedFrame reports a complete frame whose payload is not a
	// decodable envelope.
	ErrMalformedFrame = errors.New("protocol: frame is not a decodable envelope")
)

// Reader decodes length-delimited envelopes from one stream.
//
// Each frame is an unsigned-varint byte length followed by that many bytes of
// encoded Envelope. A Reader is not safe for concurrent use; one side of the
// contract reads its stream from a single goroutine.
type Reader struct {
	source *bufio.Reader
}

// NewReader reads framed envelopes from source.
func NewReader(source io.Reader) *Reader {
	return &Reader{source: bufio.NewReader(source)}
}

// ReadEnvelope reads the next envelope.
//
// It returns io.EOF when the stream ends cleanly between frames, and
// io.ErrUnexpectedEOF when it ends part-way through one. Both are ordinary
// outcomes for a peer that exited, and the caller decides what a missing
// message means.
func (r *Reader) ReadEnvelope() (*contractv1.Envelope, error) {
	length, err := binary.ReadUvarint(r.source)
	if err != nil {
		if errors.Is(err, io.EOF) {
			// ReadUvarint reports a plain EOF only when no byte of the
			// length was read; a partial length is a damaged frame.
			return nil, io.EOF
		}
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, fmt.Errorf("%w: unreadable length prefix: %w", ErrMalformedFrame, err)
	}
	// The declared length is checked before anything is allocated or read, so
	// a hostile prefix cannot drive an allocation or a long read.
	if length > MaxFrameBytes {
		return nil, fmt.Errorf("%w: declared %d bytes, limit is %d", ErrFrameTooLarge, length, MaxFrameBytes)
	}

	payload := make([]byte, length)
	if _, err := io.ReadFull(r.source, payload); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	var envelope contractv1.Envelope
	// Unknown fields are retained rather than rejected, so a newer peer may
	// add a field within this protocol version.
	if err := proto.Unmarshal(payload, &envelope); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrMalformedFrame, err)
	}
	return &envelope, nil
}

// Writer encodes length-delimited envelopes onto one stream.
//
// Every envelope is written with a single Write, and concurrent writers are
// serialized, so two messages can never interleave on the wire.
type Writer struct {
	mutex sync.Mutex
	sink  io.Writer
}

// NewWriter writes framed envelopes to sink.
func NewWriter(sink io.Writer) *Writer {
	return &Writer{sink: sink}
}

// WriteEnvelope encodes and writes one envelope.
//
// An envelope that would exceed MaxFrameBytes is rejected before any byte is
// written, so a peer never emits a frame its counterpart must reject.
func (w *Writer) WriteEnvelope(envelope *contractv1.Envelope) error {
	payload, err := proto.MarshalOptions{Deterministic: true}.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("protocol: cannot encode envelope: %w", err)
	}
	if len(payload) > MaxFrameBytes {
		return fmt.Errorf("%w: encoded %d bytes, limit is %d", ErrFrameTooLarge, len(payload), MaxFrameBytes)
	}

	var frame bytes.Buffer
	frame.Grow(len(payload) + binary.MaxVarintLen64)
	var length [binary.MaxVarintLen64]byte
	frame.Write(length[:binary.PutUvarint(length[:], uint64(len(payload)))])
	frame.Write(payload)

	w.mutex.Lock()
	defer w.mutex.Unlock()
	if _, err := w.sink.Write(frame.Bytes()); err != nil {
		return fmt.Errorf("protocol: cannot write envelope: %w", err)
	}
	return nil
}
