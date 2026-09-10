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

// The name a connect gives an account it creates (#175): the next free
// account-N, or what the user answers when something may ask.
package app_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/app"
	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/exit"
	"github.com/wso2/wso2-cli/internal/output"
)

func TestConnectNamesANewAccountAccountOneWhenNothingMayAsk(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	shell.Reader = failIfReadReader{t}

	code, out, errOut := connect(t, shell, "iam", "connect", thunderURL, "--no-input")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.Accounts[0].Name != "account-1" {
		t.Fatalf("accounts = %+v, want one named account-1", document.Accounts)
	}
	if document.Accounts[0].Auth.CredentialRef != "account-1" {
		t.Errorf("credentialRef = %q, want account-1", document.Accounts[0].Auth.CredentialRef)
	}
	if len(document.Contexts) != 1 || document.Contexts[0].Name != "account-1" ||
		document.Contexts[0].Account != "account-1" || document.DefaultContext != "account-1" {
		t.Errorf("contexts = %+v, default %q", document.Contexts, document.DefaultContext)
	}
	if strings.Contains(errOut, "Account name") {
		t.Errorf("asked for a name under --no-input:\n%s", errOut)
	}
	if !strings.Contains(out, "--account <name>") {
		t.Errorf("the report does not say how to choose the name:\n%s", out)
	}
}

func TestConnectNamesASecondAccountWithTheNextFreeNumber(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("first connect: exit %d: %s", code, errOut)
	}
	// A context holding account-2 takes that number too: the new account
	// comes with a same-named context, and only one of the two could be
	// written under it.
	if code := shell.Run([]string{"context", "create", "account-2", "--account", "account-1"}); code != exit.OK {
		t.Fatalf("context create: exit %d", code)
	}
	code, _, errOut := connect(t, shell, "iam", "connect", "http://other.example")
	if code != exit.OK {
		t.Fatalf("second connect: exit %d: %s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 2 || document.Accounts[1].Name != "account-3" ||
		document.Accounts[1].Auth.Issuer != "http://other.example" {
		t.Fatalf("accounts = %+v, want account-3 on the second issuer", document.Accounts)
	}
	if document.DefaultContext != "account-1" {
		t.Errorf("the selected context moved to %q", document.DefaultContext)
	}
}

func TestConnectAsksForTheNameAndEnterAcceptsTheDefault(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	shell.Reader = strings.NewReader("\n")

	code, out, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if !strings.Contains(errOut, "Account name [account-1]: ") {
		t.Errorf("the prompt did not offer account-1 on stderr:\n%s", errOut)
	}
	if strings.Contains(out, "Account name") {
		t.Errorf("the prompt landed on stdout:\n%s", out)
	}
	if name := loadDocument(t, shell).Accounts[0].Name; name != "account-1" {
		t.Errorf("account = %q, want account-1", name)
	}
	if !strings.Contains(out, "--account <name>") {
		t.Errorf("an accepted default is still assigned, and the report should say how to choose:\n%s", out)
	}
}

func TestConnectAsksAgainForAnInvalidOrTakenName(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("first connect: exit %d: %s", code, errOut)
	}
	t.Setenv("WSO2_NO_INPUT", "")
	shell.Reader = strings.NewReader("Other Thunder\naccount-1\nother-thunder\n")

	code, out, errOut := connect(t, shell, "iam", "connect", "http://other.example")
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if got := strings.Count(errOut, "Account name [account-2]: "); got != 3 {
		t.Errorf("asked %d times, want 3:\n%s", got, errOut)
	}
	for _, why := range []string{`"Other Thunder" cannot be used as an account name`, `"account-1" is already taken`} {
		if !strings.Contains(errOut, why) {
			t.Errorf("stderr does not say %q:\n%s", why, errOut)
		}
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 2 || document.Accounts[1].Name != "other-thunder" ||
		document.Accounts[1].Auth.CredentialRef != "other-thunder" {
		t.Fatalf("accounts = %+v, want other-thunder", document.Accounts)
	}
	if strings.Contains(out, "was assigned") {
		t.Errorf("a name the user typed is reported as assigned:\n%s", out)
	}
}

func TestConnectTakesTheDefaultWithoutAskingWhenStandardInputIsNotATerminal(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	// A nil Reader is the process's own standard input, which under go test
	// is not a terminal.
	shell.Reader = nil

	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(errOut, "Account name") {
		t.Errorf("asked with no terminal to answer:\n%s", errOut)
	}
	if name := loadDocument(t, shell).Accounts[0].Name; name != "account-1" {
		t.Errorf("account = %q, want account-1", name)
	}
}

func TestConnectTakesTheDefaultWithoutAskingUnderTheEnvironmentVariable(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")
	shell.Reader = failIfReadReader{t}

	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if name := loadDocument(t, shell).Accounts[0].Name; name != "account-1" {
		t.Errorf("account = %q, want account-1", name)
	}
}

func TestConnectStillRefusesAnAccountNameThatIsTaken(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("first connect: exit %d: %s", code, errOut)
	}
	shell.Reader = failIfReadReader{t}
	t.Setenv("WSO2_NO_INPUT", "")

	code, _, errOut := connect(t, shell, "iam", "connect", "http://other.example", "--account", "account-1")
	if code != exit.Usage || !strings.Contains(errOut, "contexts.identity_exists") ||
		!strings.Contains(errOut, "--account") {
		t.Fatalf("a taken --account: exit %d, stderr:\n%s", code, errOut)
	}
	if len(loadDocument(t, shell).Accounts) != 1 {
		t.Errorf("a refused connect wrote an account")
	}
}

func TestConnectAsksNothingWhenItCreatesNoAccount(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "1")
	if code, _, errOut := connect(t, shell, "iam", "connect", thunderURL); code != exit.OK {
		t.Fatalf("first connect: exit %d: %s", code, errOut)
	}
	t.Setenv("WSO2_NO_INPUT", "")
	shell.Reader = failIfReadReader{t}

	code, out, errOut := connect(t, shell, "apim", "connect", apimURL, "--client-id", apimClient)
	if code != exit.OK {
		t.Fatalf("exit %d: %s", code, errOut)
	}
	if strings.Contains(out, "was assigned") {
		t.Errorf("a connect onto an existing account reports a name as assigned:\n%s", out)
	}
}

// takenWhileTyping answers the prompt with a name, and before handing it back
// writes an account of that name, the way a second wso2 process would while
// the person was typing.
type takenWhileTyping struct {
	t      *testing.T
	shell  app.Shell
	answer string
	done   bool
}

func (r *takenWhileTyping) Read(buffer []byte) (int, error) {
	if r.done {
		return 0, io.EOF
	}
	r.done = true
	if code := r.shell.Run([]string{"account", "create", r.answer,
		"--issuer", "http://third.example", "--client-id", "wso2-cli"}); code != exit.OK {
		r.t.Fatalf("the concurrent account create failed: exit %d", code)
	}
	return copy(buffer, r.answer+"\n"), nil
}

func TestConnectRefusesATypedNameTakenBeforeTheWrite(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	// The concurrent writer reports through its own buffers, so the ones the
	// connect under test writes to stay its own.
	concurrent := shell
	concurrent.Streams = output.Streams{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}
	shell.Reader = &takenWhileTyping{t: t, shell: concurrent, answer: "local-thunder"}

	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code != exit.Usage || !strings.Contains(errOut, "contexts.identity_exists") {
		t.Fatalf("a typed name taken before the write: exit %d, stderr:\n%s", code, errOut)
	}
	document := loadDocument(t, shell)
	if len(document.Accounts) != 1 || document.Accounts[0].Auth.Issuer != "http://third.example" {
		t.Errorf("the connect wrote an account under another name: %+v", document.Accounts)
	}
}

// failingReader fails every read, as a standard input that breaks mid-prompt
// would.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("the terminal went away") }

func TestConnectFailsWhenThePromptCannotBeRead(t *testing.T) {
	shell, _, _ := newConnectShell(t)
	t.Setenv("WSO2_NO_INPUT", "")
	shell.Reader = failingReader{}

	code, _, errOut := connect(t, shell, "iam", "connect", thunderURL)
	if code == exit.OK || !strings.Contains(errOut, "the terminal went away") {
		t.Fatalf("a broken prompt: exit %d, stderr:\n%s", code, errOut)
	}
	if _, err := os.Stat(contexts.Path(shell.StateRoot)); !os.IsNotExist(err) {
		t.Error("a connect whose prompt failed wrote the context document")
	}
}
