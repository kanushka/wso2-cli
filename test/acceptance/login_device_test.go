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

package acceptance_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/wso2/wso2-cli/internal/auth/fakeissuer"
	"github.com/wso2/wso2-cli/internal/auth/session"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
)

// This file proves the device authorization login — the mode for a machine
// whose user has no browser that can reach it.
//
// Everything is asserted through `wso2 login` and, where the subject is what a
// module ends up holding, through a product command after it. Nothing reaches
// into the flow: what a user gets is the two streams, the exit class, what
// landed in the secure store, and what the product service was shown.
//
// The deployment's side of the polling contract is read from the fake issuer,
// which timestamps every poll it answers. That is deliberately the only place
// timing is observed — "the shell did not poll faster than I asked it to" is a
// fact about the deployment, and asserting it from there cannot be satisfied by
// a shell that merely holds the right number in a variable.

// deviceDeployment installs the standard login deployment as a device identity.
//
// Only the kind changes. That is the point of the fixture: a device context and
// a browser context differ by one word, because the schema already treats them
// as one shape and everything after the login is the same code.
func deviceDeployment(t *testing.T, options fakeissuer.Options) *loginDeployment {
	t.Helper()
	return deployLogin(t, options, func(document *contexts.Document) {
		document.Identities[0].Auth.Kind = contexts.KindOAuthDevice
	})
}

// pollableDevice is the option set every test that expects to reach the token
// endpoint starts from.
//
// The advertised interval is one second because the shell honours what the
// deployment advertises, and the RFC's own default of five would spend four of
// them proving nothing. A test that is about the default says so by leaving
// DeviceInterval unset.
func pollableDevice(outcome string, pending int) fakeissuer.Options {
	return fakeissuer.Options{
		RefreshScopeMode:    "honor",
		DeviceOutcome:       outcome,
		DevicePendingPolls:  pending,
		DeviceInterval:      1,
		RotateRefreshTokens: true,
	}
}

// userCodePattern is the shape RFC 8628 section 6.1 recommends and the fake
// issuer mints: two groups of readable upper-case letters around a separator.
var userCodePattern = regexp.MustCompile(`\b[A-Z]{4}-[A-Z]{4}\b`)

func TestADeviceLoginEstablishesASessionAndTheModuleReceivesNarrowedAccess(t *testing.T) {
	// The whole chain for the device mode, and the claim that matters most:
	// what a module ends up holding is identical to what a browser login would
	// have produced. The session carries four permissions and the module asked
	// for one, so the token it presents proves the narrowing happened on a
	// session nobody opened a browser for.
	deployment := deviceDeployment(t, pollableDevice("approve", 1))

	if code := deployment.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	afterLogin := deployment.storedSession(t)
	if afterLogin.RefreshToken == "" {
		t.Fatal("the device login stored no refresh token, so there is no session")
	}
	if afterLogin.Issuer != deployment.issuer.URL {
		t.Errorf("the stored session names issuer %q, want %q",
			afterLogin.Issuer, deployment.issuer.URL)
	}

	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	presented := deployment.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}
	active, scopes, audiences := deployment.issuer.Introspect(t, presented[0])
	if !active {
		t.Fatal("the module presented a token the issuer did not mint")
	}
	if len(scopes) != 1 || scopes[0] != referenceReadScope {
		t.Errorf("the module presented scopes %v, want exactly [%s]", scopes, referenceReadScope)
	}
	if len(audiences) != 1 || audiences[0] != referenceAudience {
		t.Errorf("the module presented audience %v, want [%s]", audiences, referenceAudience)
	}
	// The rotation-safe persistence is shared with the browser path, and this
	// proves the device path reaches it rather than bypassing it.
	if deployment.storedSession(t).RefreshToken == afterLogin.RefreshToken {
		t.Error("the derivation did not persist the rotated refresh token")
	}
}

func TestADeviceLoginPrintsTheCodeAndURIWhereTheUserCanActOnThem(t *testing.T) {
	// The two values have to survive being carried to another device — read
	// aloud, or typed by hand. So each is asserted to stand alone on its own
	// line rather than merely to appear somewhere in a paragraph.
	deployment := deviceDeployment(t, pollableDevice("approve", 0))
	if code := deployment.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, deployment.errOut)
	}

	instructions := deployment.errOut.String()
	// The instructions go to the diagnostic stream, so a user who redirects
	// standard output can still complete the login. The result stream carries
	// the report and nothing a user must act on.
	if strings.Contains(deployment.out.String(), "enter the code") {
		t.Errorf("the login instructions reached standard output:\n%s", deployment.out)
	}

	code := userCodePattern.FindString(instructions)
	if code == "" {
		t.Fatalf("no user code was printed:\n%s", instructions)
	}
	if !standsAlone(instructions, code) {
		t.Errorf("the user code %q is not on a line of its own, so it cannot be read out:\n%s",
			code, instructions)
	}
	if !standsAlone(instructions, deployment.issuer.URL+"/device") {
		t.Errorf("the verification URI is not on a line of its own:\n%s", instructions)
	}
	if !strings.Contains(instructions, "Waiting for you") {
		t.Errorf("the login never said it was waiting, so it reads as hung:\n%s", instructions)
	}
	// The complete URI is a convenience and is offered second. It carries the
	// code, so it cannot replace the two values above.
	if !strings.Contains(instructions, deployment.issuer.URL+"/device?user_code="+code) {
		t.Errorf("the advertised complete verification URI was not offered:\n%s", instructions)
	}
}

// standsAlone reports whether value occupies a line by itself, ignoring the
// indentation the instructions use. A value wrapped into a sentence is one a
// terminal may break in the middle of.
func standsAlone(text, value string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == value {
			return true
		}
	}
	return false
}

func TestNoDeviceCodeOrTokenMaterialReachesAnyOutputSurfaceOfADeviceLogin(t *testing.T) {
	// A device login prints more than a browser login does, so the
	// non-disclosure sweep is repeated for it. The device code is the addition
	// worth naming: it is exchangeable for the session, unlike the user code
	// beside it, and printing the two together would be the easy mistake.
	deployment := deviceDeployment(t, pollableDevice("approve", 0))
	if code := deployment.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	loginOut, loginErr := deployment.out.String(), deployment.errOut.String()
	afterLogin := deployment.storedSession(t)

	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	presented := deployment.service.presented()
	if len(presented) != 1 {
		t.Fatalf("the product service was shown %d bearer tokens, want 1", len(presented))
	}

	material := map[string]string{
		"the login's refresh token": afterLogin.RefreshToken,
		"the login's access token":  afterLogin.AccessToken,
		"the module's access token": presented[0],
		"the device code":           deployment.issuer.LastDeviceCode(),
	}
	surfaces := map[string]string{
		"login standard output":  loginOut,
		"login diagnostics":      loginErr,
		"status standard output": deployment.out.String(),
		"status diagnostics":     deployment.errOut.String(),
	}
	for label, secret := range material {
		if secret == "" {
			t.Fatalf("%s is empty, so this sweep scans for nothing", label)
		}
		for surface, stream := range surfaces {
			if strings.Contains(stream, secret) {
				t.Errorf("%s appeared on %s:\n%s", label, surface, stream)
			}
		}
	}
}

func TestADeclinedDeviceLoginAndAnAbandonedOneReadDifferently(t *testing.T) {
	// Both leave the user holding no session, so both carry the same code —
	// the shell's codes name where a caller is left, not what the deployment
	// said. What differs is the sentence, because the two readers have
	// different things to do: one of them chose this, and the other ran out of
	// time.
	for name, testcase := range map[string]struct {
		options fakeissuer.Options
		expect  string
		reject  string
	}{
		"declined at the identity provider": {
			options: pollableDevice("deny", 0),
			expect:  "declined",
			reject:  "closed before",
		},
		"the approval window closed": {
			options: pollableDevice("expire", 0),
			expect:  "closed before",
			reject:  "declined",
		},
		"never approved before the code expired": {
			// The deployment keeps answering "pending" and the code's own
			// lifetime runs out. The shell stops because the deployment said
			// when it would stop being worth asking, which is a different path
			// through the flow than an expired_token answer.
			options: fakeissuer.Options{
				RefreshScopeMode:   "honor",
				DevicePendingPolls: 1000,
				DeviceInterval:     3,
				DeviceExpiresIn:    1,
			},
			expect: "in time",
			reject: "declined",
		},
	} {
		t.Run(name, func(t *testing.T) {
			deployment := deviceDeployment(t, testcase.options)

			if code := deployment.shell.Run([]string{"login"}); code != exitAuthPolicy {
				t.Fatalf("wso2 login exited %d, want the authentication class %d\nstderr:\n%s",
					code, exitAuthPolicy, deployment.errOut)
			}
			refusal := deployment.errOut.String()
			if !strings.Contains(refusal, "auth.credential_unavailable") {
				t.Errorf("the refusal did not carry auth.credential_unavailable:\n%s", refusal)
			}
			if !strings.Contains(refusal, testcase.expect) {
				t.Errorf("the refusal does not say %q:\n%s", testcase.expect, refusal)
			}
			if strings.Contains(refusal, testcase.reject) {
				t.Errorf("the refusal reads as the wrong ending, saying %q:\n%s",
					testcase.reject, refusal)
			}
			if _, err := (session.Store{StateRoot: deployment.stateRoot}).
				Load(loginCredentialRef); err == nil {
				t.Error("a device login that never completed stored a session anyway")
			}
		})
	}
}

func TestADeploymentWithoutTheDeviceGrantIsRefusedBeforeAnyCodeIsShown(t *testing.T) {
	// Thunder is such a deployment today. The refusal has to come before the
	// instructions, because a user who has already been given a code will go
	// and look for an approval screen that does not exist, and will blame
	// themselves when they cannot find it.
	deployment := deviceDeployment(t, fakeissuer.Options{
		RefreshScopeMode:   "honor",
		OmitDeviceEndpoint: true,
	})

	if code := deployment.shell.Run([]string{"login"}); code != exitAuthPolicy {
		t.Fatalf("wso2 login exited %d, want the authentication class %d\nstderr:\n%s",
			code, exitAuthPolicy, deployment.errOut)
	}
	refusal := deployment.errOut.String()
	if !strings.Contains(refusal, "auth.discovery_failed") {
		t.Errorf("the refusal did not carry auth.discovery_failed:\n%s", refusal)
	}
	if !strings.Contains(refusal, "does not advertise the device authorization grant") {
		t.Errorf("the refusal does not name the missing grant:\n%s", refusal)
	}
	if userCodePattern.MatchString(refusal) || strings.Contains(refusal, "enter the code") {
		t.Errorf("a code was shown for a login that could never be approved:\n%s", refusal)
	}
	if polls := deployment.issuer.DevicePolls(); len(polls) != 0 {
		t.Errorf("the shell polled the token endpoint %d times without a device grant", len(polls))
	}
}

func TestADeviceLoginHonoursTheAdvertisedIntervalAndBacksOffWhenTold(t *testing.T) {
	// The deployment's protection against a fleet of shells. Asserted from the
	// deployment's own record of when each poll arrived, because that is the
	// only place the property is real.
	//
	// This test spends real seconds, and cannot avoid it: RFC 8628 fixes the
	// back-off at five seconds and the shell honours the RFC rather than a knob
	// this suite could turn down. It is the one test here that waits, and it
	// waits once.
	deployment := deviceDeployment(t, fakeissuer.Options{
		RefreshScopeMode:    "honor",
		DeviceInterval:      1,
		DeviceSlowDownPolls: 1,
		DeviceOutcome:       "approve",
	})

	started := time.Now()
	if code := deployment.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	polls := deployment.issuer.DevicePolls()
	if len(polls) != 2 {
		t.Fatalf("the deployment was polled %d times, want 2 (one refused with slow_down, "+
			"one approved)", len(polls))
	}
	// The flow waits one interval before asking at all, which is what keeps a
	// deployment from being hit the instant it issues a code.
	if first := polls[0].Sub(started); first < time.Second {
		t.Errorf("the first poll arrived after %v, sooner than the advertised 1s interval", first)
	}
	// The back-off is the point: after slow_down the gap must exceed what was
	// advertised, and by RFC 8628 section 3.5 it grows by five seconds.
	gap := polls[1].Sub(polls[0])
	if gap < 5*time.Second {
		t.Errorf("the poll after slow_down came %v later; the interval did not grow by the "+
			"five seconds RFC 8628 requires", gap)
	}
}

func TestADeviceLoginWithoutAnIdentityTokenStillEstablishesASession(t *testing.T) {
	// Deliberately unlike the browser login, which refuses without a verified
	// identity token. RFC 8628 defines no nonce and whether WSO2 deployments
	// return an identity token from this grant is not measured, so the session
	// — which is the refresh token — is not made to depend on one. What the
	// shell must not do is claim a subject it never verified.
	deployment := deviceDeployment(t, fakeissuer.Options{
		RefreshScopeMode:  "honor",
		DeviceInterval:    1,
		OmitDeviceIDToken: true,
	})

	if code := deployment.shell.Run([]string{"login"}); code != exit.OK {
		t.Fatalf("wso2 login exited %d\nstderr:\n%s", code, deployment.errOut)
	}
	if deployment.storedSession(t).RefreshToken == "" {
		t.Fatal("no session was stored, so an unmeasured issuer behaviour decided the login")
	}
	report := deployment.out.String()
	if strings.Contains(report, "Subject") {
		t.Errorf("the report claims a subject no identity token proved:\n%s", report)
	}
	if !strings.Contains(report, "Logged in") {
		t.Errorf("the login did not report success:\n%s", report)
	}
	// The session still reaches a module, which is the whole reason the missing
	// claim is tolerated rather than refused over.
	if code := deployment.status(t); code != exit.OK {
		t.Fatalf("reference status exited %d\nstderr:\n%s", code, deployment.errOut)
	}
}
