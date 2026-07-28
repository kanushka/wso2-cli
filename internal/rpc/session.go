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

package rpc

import (
	"errors"
	"fmt"
	"io"
	"slices"

	"github.com/wso2/wso2-cli/internal/auth"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/protocol"
	"github.com/wso2/wso2-cli/sdk/protocol/contractv1"
)

// Session speaks the module contract over one pair of streams.
//
// It knows nothing about processes, so the same code path is exercised by unit
// tests over in-memory streams and by a real module over its standard streams.
type Session struct {
	// Resolved is the installation the shell decided to launch. Its receipt
	// is the claim the running module's identity is checked against.
	Resolved modules.Resolved
	// Shell is the identity reported to the module.
	Shell ShellIdentity
	// InvocationID binds every post-handshake message to this invocation.
	InvocationID string
	// Broker answers the module's access requests. A session without one
	// denies access rather than granting it.
	Broker Broker
}

// Broker is the shell's authentication policy as the session sees it.
//
// It is an interface so the protocol can be tested without the credential
// handling behind it, and so the session cannot reach past the one question it
// is entitled to ask. internal/auth implements it.
type Broker interface {
	// Acquire applies broker policy to one request, returning the access to
	// grant or the typed problem to refuse it with.
	Acquire(request auth.Request) (auth.Grant, error)
}

// Run performs the handshake, sends one invocation, and returns the module's
// single terminal outcome.
//
// Every failure it returns is a typed problem, so the caller never has to
// invent a category for a protocol fault.
func (s Session) Run(toModule io.Writer, fromModule io.Reader, invocation Invocation) (Outcome, error) {
	reader := protocol.NewReader(fromModule)
	writer := protocol.NewWriter(toModule)

	if err := s.handshake(reader, writer); err != nil {
		return Outcome{}, err
	}
	if err := s.sendInvoke(writer, invocation); err != nil {
		return Outcome{}, err
	}
	return s.readTerminal(reader, writer)
}

// handshake reads the module's hello, proves the running module is the one the
// receipt describes, and answers with a welcome.
func (s Session) handshake(reader *protocol.Reader, writer *protocol.Writer) error {
	envelope, err := reader.ReadEnvelope()
	if err != nil {
		return s.readProblem(err, "while waiting for the module's handshake")
	}
	hello := envelope.GetHello()
	if hello == nil {
		return processProblem("rpc.unexpected_message",
			fmt.Sprintf("the %q module opened with %s instead of a handshake", s.namespace(), protocol.DescribeMessage(envelope)),
			retryRecovery)
	}
	if err := s.checkRuntimeIdentity(hello); err != nil {
		return err
	}
	if err := s.checkRequiredCapabilities(hello); err != nil {
		return err
	}
	if err := s.checkRuntimeProtocol(hello); err != nil {
		return err
	}

	return s.write(writer, "the handshake", &contractv1.Envelope{
		InvocationId: s.InvocationID,
		Message: &contractv1.Envelope_Welcome{Welcome: &contractv1.Welcome{
			ProtocolVersion: uint32(s.Resolved.ProtocolVersion),
			Shell: &contractv1.ShellIdentity{
				Version:  s.Shell.Version,
				Platform: s.Shell.Platform,
			},
		}},
	})
}

// write sends one envelope, reporting a failed send as a typed problem.
//
// A module that closed or ignored its protocol input makes the shell's write
// fail. That is a module process failure like any other, not an internal error
// the caller has to classify.
func (s Session) write(writer *protocol.Writer, description string, envelope *contractv1.Envelope) error {
	if err := writer.WriteEnvelope(envelope); err != nil {
		return processProblem("rpc.stream_failed",
			fmt.Sprintf("the shell could not send %s to the %q module", description, s.namespace()),
			retryRecovery)
	}
	return nil
}

// checkRuntimeIdentity proves the executable that started is the one the
// receipt describes.
//
// The digest check proved the file's bytes are unchanged. This proves the
// running program agrees about what it is, so an installation that describes
// one module while running another is refused before any command is sent.
func (s Session) checkRuntimeIdentity(hello *contractv1.Hello) error {
	receipt := s.Resolved.Receipt
	identity := hello.GetModule()
	if identity.GetNamespace() != receipt.Namespace {
		return trustProblem("rpc.identity_mismatch",
			fmt.Sprintf("the executable installed as the %q module reports namespace %q",
				receipt.Namespace, identity.GetNamespace()),
			reinstallRecovery)
	}
	if identity.GetVersion() != receipt.ModuleVersion {
		return trustProblem("rpc.identity_mismatch",
			fmt.Sprintf("the installed %q module is recorded as version %s, and the executable reports %q",
				receipt.Namespace, receipt.ModuleVersion, identity.GetVersion()),
			reinstallRecovery)
	}
	return nil
}

// checkRequiredCapabilities fails closed on any behaviour the module requires
// and this shell does not provide, so a module never runs with a capability it
// depends on silently absent.
func (s Session) checkRequiredCapabilities(hello *contractv1.Hello) error {
	for _, capability := range hello.GetRequiredCapabilities() {
		if _, supported := supportedCapabilities[capability]; !supported {
			return trustProblem("rpc.unsupported_capability",
				fmt.Sprintf("the %q module requires the %q protocol capability, which this shell does not provide",
					s.namespace(), capability),
				"Update the WSO2 CLI, or install a module version that this shell supports.")
		}
	}
	return nil
}

// checkRuntimeProtocol proves the running module speaks the version its receipt
// promised and the shell selected.
//
// The version is chosen during resolution, from the receipt and this shell's
// own list, and the handshake confirms it rather than reopening it. A module
// that offers a newer shared version at runtime is therefore not negotiated up
// to it: doing so would let the executable widen what its installation
// declared, which is exactly the drift the receipt exists to prevent. A newer
// version becomes available by reinstalling, not by the module asking.
func (s Session) checkRuntimeProtocol(hello *contractv1.Hello) error {
	offered := protocol.VersionsOf(hello)
	if slices.Contains(offered, s.Resolved.ProtocolVersion) {
		return nil
	}
	return trustProblem("rpc.protocol_mismatch",
		fmt.Sprintf("the installed %q module is recorded as speaking module-contract protocol v%d, and the executable offers %s",
			s.namespace(), s.Resolved.ProtocolVersion, protocol.FormatVersions(offered)),
		reinstallRecovery)
}

// sendInvoke asks the module to run exactly one command.
func (s Session) sendInvoke(writer *protocol.Writer, invocation Invocation) error {
	return s.write(writer, "the invocation", &contractv1.Envelope{
		InvocationId: s.InvocationID,
		Message: &contractv1.Envelope_Invoke{Invoke: &contractv1.Invoke{
			Namespace:   invocation.Namespace,
			CommandPath: invocation.Command,
			Arguments:   invocation.Arguments,
			OutputMode:  protocol.EncodeOutputMode(invocation.OutputMode),
			Policy: &contractv1.InvocationPolicy{
				DeadlineMillis: uint32(invocation.timeout().Milliseconds()),
				Interactive:    invocation.Interactive,
			},
			Context: &contractv1.InvocationContext{
				Name:           invocation.Context.Name,
				OrganizationId: invocation.Context.OrganizationID,
				Endpoint:       invocation.Context.Endpoint,
			},
		}},
	})
}

// readTerminal reads the module's single terminal message and proves nothing
// follows it.
//
// A module may ask the broker for access before it answers, so every message is
// read until the terminal one arrives. Access requests are the only thing that
// may precede it: anything else is a module the shell and this release disagree
// about, and is refused.
func (s Session) readTerminal(reader *protocol.Reader, writer *protocol.Writer) (Outcome, error) {
	var envelope *contractv1.Envelope
	// issued holds each refusal as the shell would report it. The module was
	// sent a narrower version, so what a user reads about a refusal comes from
	// here rather than from whatever the module hands back.
	var issued []problem.Problem
	requests := 0
	for {
		read, err := reader.ReadEnvelope()
		if err != nil {
			return Outcome{}, s.readProblem(err, "while waiting for the module's result")
		}
		if read.GetInvocationId() != s.InvocationID {
			return Outcome{}, processProblem("rpc.invocation_mismatch",
				fmt.Sprintf("the %q module answered for another invocation", s.namespace()),
				retryRecovery)
		}
		if read.GetAcquireAccess() == nil {
			envelope = read
			break
		}
		// Broker traffic is bounded like every other input the shell reads
		// from a module. The invocation deadline would end a module that asked
		// forever, but only after holding the command open for it; a module
		// with nothing left to ask has already had every answer it will get.
		requests++
		if requests > MaxAccessRequests {
			return Outcome{}, processProblem("rpc.too_many_access_requests",
				fmt.Sprintf("the %q module asked for access more than %d times without answering",
					s.namespace(), MaxAccessRequests),
				retryRecovery)
		}
		// An access request is one half of an exchange, so it has to say which
		// exchange. Answering an uncorrelated request would leave the module
		// unable to tell this answer from any other, and the shell unable to
		// prove it answered what was asked.
		if read.GetCorrelationId() == "" {
			return Outcome{}, processProblem("rpc.uncorrelated_access_request",
				fmt.Sprintf("the %q module asked for access without a correlation identifier",
					s.namespace()),
				retryRecovery)
		}
		if err := s.answerAccess(writer, read, &issued); err != nil {
			return Outcome{}, err
		}
	}

	outcome := Outcome{InvocationID: s.InvocationID}
	switch {
	case envelope.GetResult() != nil:
		decoded := protocol.DecodeResult(envelope.GetResult())
		// A result is a peer's claim, so it is validated before it can reach
		// a renderer that assumes named, unique fields.
		if err := decoded.Validate(); err != nil {
			return Outcome{}, processProblem("rpc.invalid_result",
				fmt.Sprintf("the %q module returned a result the shell cannot render: %s", s.namespace(), err),
				retryRecovery)
		}
		outcome.Result = decoded
	case envelope.GetProblem() != nil:
		decoded, err := protocol.DecodeProblem(envelope.GetProblem())
		if err != nil {
			return Outcome{}, processProblem("rpc.invalid_problem",
				fmt.Sprintf("the %q module returned a failure the shell cannot report: %s", s.namespace(), err),
				retryRecovery)
		}
		if restored, found := restoreDenial(issued, decoded); found {
			decoded = restored
		}
		outcome.Problem = &decoded
	default:
		return Outcome{}, processProblem("rpc.unexpected_message",
			fmt.Sprintf("the %q module ended with %s instead of a result", s.namespace(), protocol.DescribeMessage(envelope)),
			retryRecovery)
	}

	if err := s.expectNoFurtherMessages(reader); err != nil {
		return Outcome{}, err
	}
	return outcome, nil
}

// answerAccess applies broker policy to one access request and answers it.
//
// The shell answers every request, and the answer is always typed: a module is
// never left waiting, and it never has to guess whether silence meant refusal.
// The module's own request is not what decides the outcome — the broker weighs
// it against the module receipt, the selected context, and this invocation.
func (s Session) answerAccess(
	writer *protocol.Writer,
	envelope *contractv1.Envelope,
	issued *[]problem.Problem,
) error {
	request := envelope.GetAcquireAccess()
	grant, err := s.acquire(auth.Request{
		Audience: request.GetAudience(),
		Scopes:   request.GetScopes(),
	})
	if err != nil {
		sent, reported := s.denial(err)
		*issued = append(*issued, reported)
		return s.write(writer, "the access denial", &contractv1.Envelope{
			InvocationId:  s.InvocationID,
			CorrelationId: envelope.GetCorrelationId(),
			Message: &contractv1.Envelope_AccessDenied{AccessDenied: &contractv1.AccessDenied{
				Problem: protocol.EncodeProblem(sent),
			}},
		})
	}
	return s.write(writer, "the access grant", &contractv1.Envelope{
		InvocationId:  s.InvocationID,
		CorrelationId: envelope.GetCorrelationId(),
		Message: &contractv1.Envelope_AccessGranted{AccessGranted: &contractv1.AccessGranted{
			Token:         grant.Token,
			ExpiresAtUnix: grant.ExpiresAt.Unix(),
		}},
	})
}

// acquire consults the broker, refusing when this invocation has none.
//
// A session with no broker is a session that can authorize nothing, so it
// refuses rather than falling through to a grant.
func (s Session) acquire(request auth.Request) (auth.Grant, error) {
	if s.Broker == nil {
		return auth.Grant{}, problem.New(problem.CategoryAuthPolicy, "auth.broker_unavailable",
			fmt.Sprintf("the %q module asked for access, and this command brokered none", s.namespace())).
			WithRecovery("Retry the command. Report the failure if it persists.")
	}
	return s.Broker.Acquire(request)
}

// denial renders a broker failure twice: as the module receives it, and as the
// shell would report it.
//
// The two differ when recovery guidance names something the user needs and the
// module may not have, such as the context's credential source. Everything the
// module is sent crosses the contract, so the narrower version is the one that
// travels.
//
// A failure the broker did not type is still an access failure: the invocation
// was not authorized, and reporting it as anything else would let an internal
// fault look like a granted request that went wrong later.
func (s Session) denial(err error) (sent, reported problem.Problem) {
	var refused auth.Denial
	if errors.As(err, &refused) {
		return refused.Problem, refused.Reported()
	}
	var typed problem.Problem
	if errors.As(err, &typed) {
		return typed, typed
	}
	undecided := problem.New(problem.CategoryAuthPolicy, "auth.access_undecided",
		fmt.Sprintf("the shell could not decide the %q module's access", s.namespace())).
		WithRecovery("Retry the command. Report the failure if it persists.")
	return undecided, undecided
}

// restoreDenial replaces a module's account of a refusal with the shell's own.
//
// A module returning a denial it was sent has nothing to add to it: the shell
// decided the refusal, so the shell's version is the authoritative one, and it
// may carry guidance the module was deliberately not given. Only a refusal this
// invocation actually issued is restored, so nothing a module invents can make
// the shell speak for it.
func restoreDenial(issued []problem.Problem, returned problem.Problem) (problem.Problem, bool) {
	for _, refusal := range issued {
		if refusal.Code == returned.Code && refusal.Category == returned.Category {
			return refusal, true
		}
	}
	return problem.Problem{}, false
}

// expectNoFurtherMessages proves the module stopped after its terminal message.
//
// Extra protocol output means the module and the shell disagree about where the
// invocation ended, so the exchange is refused rather than partly believed.
func (s Session) expectNoFurtherMessages(reader *protocol.Reader) error {
	_, err := reader.ReadEnvelope()
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return s.readProblem(err, "after the module's terminal message")
	}
	return processProblem("rpc.extra_message",
		fmt.Sprintf("the %q module kept writing protocol frames after its result", s.namespace()),
		retryRecovery)
}

// readProblem turns a framing failure into the typed problem that describes it.
func (s Session) readProblem(err error, when string) error {
	switch {
	case errors.Is(err, io.EOF):
		return processProblem("rpc.no_terminal_message",
			fmt.Sprintf("the %q module stopped %s without returning a result", s.namespace(), when),
			retryRecovery)
	case errors.Is(err, io.ErrUnexpectedEOF):
		return processProblem("rpc.truncated_message",
			fmt.Sprintf("the %q module stopped part-way through a message %s", s.namespace(), when),
			retryRecovery)
	case errors.Is(err, protocol.ErrFrameTooLarge):
		return processProblem("rpc.oversized_message",
			fmt.Sprintf("the %q module sent a message larger than the shell accepts", s.namespace()),
			retryRecovery)
	case errors.Is(err, protocol.ErrMalformedFrame):
		return processProblem("rpc.malformed_message",
			fmt.Sprintf("the %q module sent a message the shell cannot decode", s.namespace()),
			retryRecovery)
	default:
		return processProblem("rpc.stream_failed",
			fmt.Sprintf("the connection to the %q module failed %s", s.namespace(), when),
			retryRecovery)
	}
}

func (s Session) namespace() string {
	return s.Resolved.Receipt.Namespace
}
