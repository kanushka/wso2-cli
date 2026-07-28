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

package module

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
)

// AccessRequest is what a module asks the shell's broker for.
//
// A module states what it intends to do, never how the shell should authorize
// it: there is no place here for a credential, a credential source, an identity
// provider, or a token the module obtained elsewhere.
type AccessRequest struct {
	// Audience is the single protected audience the module intends to call.
	// The shell grants it only if the module receipt declares it.
	Audience string
	// Scopes are the permissions the module needs. The shell grants them only
	// if the module receipt declares them.
	Scopes []string
}

// Access is what the broker granted.
//
// It is deliberately only the access material and its expiry. A module cannot
// reach the credential behind it and cannot renew it: when access expires, the
// work belongs to a new command, where the shell applies policy again.
type Access struct {
	// Token is the access material to present to the audience.
	Token string
	// ExpiresAt is when the token stops being accepted. A module may use it to
	// fail early; the audience enforces it regardless.
	ExpiresAt time.Time
}

// Broker is the shell's authentication broker as a handler sees it.
//
// It is an interface so a module author can test a handler against a stub, and
// so the SDK can replace the implementation without changing handlers.
type Broker interface {
	// Acquire asks the shell for access. It returns the granted access, or a
	// problem.Problem carrying the shell's typed denial. A handler that cannot
	// continue without access should return that problem unchanged: the denial
	// is shell policy, and the shell renders it and chooses the exit class.
	Acquire(ctx context.Context, request AccessRequest) (Access, error)
}

// streamBroker asks the shell for access over the module contract.
//
// It reuses the invocation's streams, so a request and its answer are ordinary
// contract messages: there is no side channel, no socket, and nothing for a
// module to connect to on its own.
type streamBroker struct {
	reader       *protocol.Reader
	writer       *protocol.Writer
	invocationID string

	// mutex serializes exchanges. A handler may call Acquire from more than
	// one goroutine, and two overlapping request/answer pairs on one stream
	// would let a request read the other's answer.
	mutex    sync.Mutex
	requests int
}

// Acquire performs one broker exchange.
//
// The wait for the shell's answer carries no timer of its own, and does not
// need one. The shell closes this module's protocol input on every path it can
// end an invocation by — a granted command, its own deadline, cancellation, or
// the shell process dying — and that closure ends the wait with an error. A
// module left waiting is also a module the shell has already stopped waiting
// for: it terminates the process after a short grace period.
//
// Bounding the read on ctx instead would not improve on that. A framed read
// abandoned part-way leaves the stream at an unknown position, so nothing may
// be read from it afterwards; the only safe response to a cancelled read is to
// end the invocation, which closing the input already does. The handshake and
// invocation reads are unbounded for the same reason.
func (b *streamBroker) Acquire(ctx context.Context, request AccessRequest) (Access, error) {
	if err := ctx.Err(); err != nil {
		return Access{}, fmt.Errorf("module: access was not requested: %w", err)
	}

	b.mutex.Lock()
	defer b.mutex.Unlock()

	b.requests++
	correlationID := "acquire-" + strconv.Itoa(b.requests)
	if err := b.writer.WriteEnvelope(&contractv1.Envelope{
		InvocationId:  b.invocationID,
		CorrelationId: correlationID,
		Message: &contractv1.Envelope_AcquireAccess{AcquireAccess: &contractv1.AcquireAccess{
			Audience: request.Audience,
			Scopes:   request.Scopes,
		}},
	}); err != nil {
		return Access{}, fmt.Errorf("module: cannot ask the shell for access: %w", err)
	}

	envelope, err := b.reader.ReadEnvelope()
	if err != nil {
		return Access{}, fmt.Errorf("module: cannot read the shell's answer to an access request: %w", err)
	}
	// The answer is proved to be this invocation's answer to this request
	// before it is believed, so a misrouted or replayed grant cannot be used.
	if envelope.GetInvocationId() != b.invocationID {
		return Access{}, errors.New("module: the shell answered an access request for another invocation")
	}
	if envelope.GetCorrelationId() != correlationID {
		return Access{}, errors.New("module: the shell answered an access request that was not made")
	}

	switch {
	case envelope.GetAccessGranted() != nil:
		granted := envelope.GetAccessGranted()
		if granted.GetToken() == "" {
			return Access{}, errors.New("module: the shell granted access without any access material")
		}
		return Access{
			Token:     granted.GetToken(),
			ExpiresAt: time.Unix(granted.GetExpiresAtUnix(), 0).UTC(),
		}, nil
	case envelope.GetAccessDenied() != nil:
		denial, decodeErr := protocol.DecodeProblem(envelope.GetAccessDenied().GetProblem())
		if decodeErr != nil {
			return Access{}, fmt.Errorf("module: the shell denied access unreportably: %w", decodeErr)
		}
		return Access{}, denial
	default:
		return Access{}, fmt.Errorf("module: the shell answered an access request with %s",
			protocol.DescribeMessage(envelope))
	}
}
