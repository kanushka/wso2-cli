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

package app

import (
	"fmt"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// kindGate refuses an identity whose authentication kind holds no interactive
// session, on behalf of one command.
//
// wso2 login and wso2 logout accept exactly the same two kinds and refuse every
// other one for the same reasons, so the decision lives here once. Kept apart,
// the two would drift on which kinds hold a session — and the answer to that has
// to be the same for the command that establishes one and the command that ends
// one, or a user could log in somewhere they cannot log out of.
type kindGate struct {
	// command is the command's own name, for recovery text that names the way
	// back to the command the user actually ran.
	command string
	// purpose completes "no WSO2 CLI context is selected ...", because a
	// context is selected to log in to and selected to log out of.
	purpose string
	// unselected is the way out of that refusal. It differs between the two
	// commands because the way out does: for login, creating the context is
	// the command itself, and for logout there is nothing to end until a login
	// has created something.
	unselected string
	// inline refuses an identity that acquires access inline and so never holds
	// a session. It is the one branch the two commands genuinely disagree
	// about: there is nothing to establish, and there is nothing to end, and
	// those are different things to be told.
	inline func(contextName string) problem.Problem
}

// check reports why the selected identity cannot hold an interactive session,
// or nil when it can.
func (g kindGate) check(selected contexts.Selection) error {
	switch selected.Identity.Auth.Kind {
	case "":
		return problem.New(problem.CategoryAuthPolicy, "auth.context_not_selected",
			"no WSO2 CLI context is selected "+g.purpose).
			// It names commands rather than a file. Telling a user to author a
			// context document was the only advice available when nothing in
			// the shell could write one; wso2 login and wso2 context now can,
			// and this is the first refusal a user on a clean machine meets.
			WithRecovery(g.unselected)
	case contexts.KindClientCredentials, contexts.MethodDevelopmentCredential:
		return g.inline(selected.Context.Name)
	case contexts.KindPAT:
		return problem.New(problem.CategoryAuthPolicy, "auth.kind_not_implemented",
			fmt.Sprintf("the %q context uses an authentication kind this release does not implement",
				selected.Context.Name)).
			WithRecovery("Use a browser, device-code, or client-credentials identity. Personal " +
				"access token login is planned.")
	case contexts.KindOAuthBrowser, contexts.KindOAuthDevice:
		// The two kinds this release establishes and ends sessions for.
		return nil
	default:
		return problem.New(problem.CategoryAuthPolicy, "auth.method_unsupported",
			fmt.Sprintf("the %q context uses an authentication method this shell does not implement",
				selected.Context.Name)).
			WithRecovery("Select a context with a supported authentication kind.")
	}
}

// loginKindGate refuses an identity wso2 login cannot establish a session for.
var loginKindGate = kindGate{
	command: "login",
	unselected: "Run wso2 login --url <issuer> --client-id <id> to log in and create the " +
		"identity and context it authenticates, or wso2 context use <name> to select a " +
		"context that is already configured. wso2 context list shows what is configured.",
	purpose: "to log in to",
	inline: func(contextName string) problem.Problem {
		return problem.New(problem.CategoryAuthPolicy, "auth.login_not_required",
			fmt.Sprintf("the %q context acquires access inline and has no login step", contextName)).
			WithRecovery("Run the product command directly; the shell authenticates during it.")
	},
}

// logoutKindGate refuses an identity wso2 logout has no session to end for.
var logoutKindGate = kindGate{
	command: "logout",
	unselected: "Run wso2 context use <name> to select a configured context, or wso2 context " +
		"list to see what is configured. Nothing holds a session until wso2 login has " +
		"established one.",
	purpose: "to log out of",
	inline: func(contextName string) problem.Problem {
		return problem.New(problem.CategoryAuthPolicy, "auth.logout_not_required",
			fmt.Sprintf("the %q context acquires access inline and holds no session to end",
				contextName)).
			WithRecovery("Nothing is stored for this context. Remove the credential from the " +
				"environment to stop the shell acquiring access with it.")
	},
}
