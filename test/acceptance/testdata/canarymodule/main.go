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

// Command canarymodule is a conforming module that discloses everything it can
// reach, used by the shell's acceptance canary scan.
//
// The reference module is written to be well behaved, so it proves only that a
// careful module keeps a credential safe. The stronger claim the architecture
// proof has to make is that a careless or hostile module cannot disclose one
// even when it tries, because it was never given anything to disclose. This
// module is that adversary: it takes every value the shell hands a module —
// process arguments, the whole environment, the invocation, the non-secret
// context, and the access it is granted — and pushes all of it out through both
// channels a module has, its diagnostics and its result.
//
// The scan then reads that output looking for the harness's canary credential.
// Finding it anywhere is a disclosure; finding the granted token instead is the
// boundary working, because a token is what a module is meant to hold.
//
// It claims the reference namespace and version so the shell resolves and
// launches it exactly as it would the reference module. It is fixture code and
// never ships.
package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The audience and scope the reference receipt declares. This module asks for
// exactly them, so it is granted access and the granted token becomes one more
// surface for the scan to read.
const (
	statusAudience = "reference-status"
	statusScope    = "reference:status:read"
)

// DisclosurePrefix opens every disclosure line on standard error, so the scan
// can tell what this module said from what the SDK or the runtime said.
const DisclosurePrefix = "canary-disclosure: "

// Schema is the result schema this module answers with. It is deliberately not
// the reference status schema: this is not a status answer.
const Schema = "acceptance.canary/v1"

// moduleVersion is injected by the acceptance test so this module matches the
// receipt it is installed under.
var moduleVersion = "0.0.0-dev"

func main() {
	err := module.Serve(context.Background(),
		module.Options{
			Namespace:     "reference",
			Version:       moduleVersion,
			AuthAudiences: []string{statusAudience},
			AuthScopes:    []string{statusScope},
		},
		// Both names reach the same handler, so this fixture mirrors the
		// command surface the real reference module serves. A test that
		// arranges several module sources behind one invocation — canary_test's
		// refusal table does — must be able to name one command that every
		// arrangement answers.
		module.Command{Path: []string{"status"}, Run: disclose},
		module.Command{Path: []string{"call"}, Run: disclose})
	if err != nil {
		fmt.Fprintf(os.Stderr, "canarymodule: %v\n", err)
		os.Exit(1)
	}
}

// disclose reports everything this process can observe.
//
// A failure to acquire access is not a failure of this handler: a denial is
// itself a surface the scan reads, so the denial text is disclosed like
// everything else and the run still answers.
func disclose(ctx context.Context, request module.Request) (result.Result, error) {
	access, acquireErr := request.Access.Acquire(ctx, module.AccessRequest{
		Audience: statusAudience,
		Scopes:   []string{statusScope},
	})

	observed := []struct{ name, value string }{
		{"arguments", strings.Join(os.Args, " ")},
		{"environment", environment()},
		{"invocation", request.InvocationID},
		{"command", strings.Join(request.Command, " ")},
		{"userArguments", strings.Join(request.Arguments, " ")},
		{"outputMode", string(request.OutputMode)},
		{"contextName", request.Context.Name},
		{"contextOrganization", request.Context.OrganizationID},
		{"contextEndpoint", request.Context.Endpoint},
		{"accessToken", access.Token},
		{"accessExpiry", expiry(access.ExpiresAt)},
		{"accessError", errorText(acquireErr)},
	}

	// Both channels carry the same values. Standard error is what a module
	// says about itself; the result is what the shell renders for the user.
	// A credential that reached this module would have to escape through one
	// of them, so the scan reads both.
	disclosed := result.New(Schema)
	for _, entry := range observed {
		fmt.Fprintf(os.Stderr, "%s%s=%s\n", DisclosurePrefix, entry.name, entry.value)
		disclosed = disclosed.With(entry.name, entry.name, entry.value)
	}
	return disclosed, nil
}

// environment renders this process's whole environment as one value, in a
// stable order so a failure report reads the same way twice.
func environment() string {
	entries := os.Environ()
	sort.Strings(entries)
	return strings.Join(entries, " ")
}

func expiry(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.UTC().Format(time.RFC3339)
}

func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
