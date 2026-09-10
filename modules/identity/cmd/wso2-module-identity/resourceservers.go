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

package main

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// ResourceServersSchema and ResourceServerSchema identify the semantic shapes
// of a listing and of one created resource server.
const (
	ResourceServersSchema = "identity.resourceServers/v1"
	ResourceServerSchema  = "identity.resourceServer/v1"
)

// defaultDelimiter is the character ThunderID composes a permission name with
// when a resource server does not state its own. A handle carrying it is
// refused, because the deployment composes rather than escapes.
const defaultDelimiter = ":"

type resourceServer struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Identifier string `json:"identifier"`
	Delimiter  string `json:"delimiter"`
	OUID       string `json:"ouId"`
}

type resourceServerListing struct {
	TotalResults    int              `json:"totalResults"`
	ResourceServers []resourceServer `json:"resourceServers"`
}

// resourceServersList answers "wso2 identity resource-servers list".
func resourceServersList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := clientFor(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listing resourceServerListing
	if err := client.Get(ctx, "/resource-servers", &listing); err != nil {
		return result.Result{}, callFailed(err, "read the resource servers", request.Context.Endpoint)
	}
	report := result.New(ResourceServersSchema).
		With("count", "Resource servers", strconv.Itoa(listing.TotalResults)).
		WithColumn("name", "Name").
		WithColumn("identifier", "Identifier").
		WithColumn("id", "ID")
	for _, rs := range listing.ResourceServers {
		// The identifier is second, because it is the value an operator copies
		// into an account's product record as the audience.
		report = report.WithRow(rs.Name, rs.Identifier, rs.ID)
	}
	return report.With(NextField, "Next",
		"Record one on an account with wso2 account add-product <account> <namespace> "+
			"--endpoint <url> --audience <identifier>."), nil
}

// resourceServerFlags are what create was asked for. They are bound to the
// Cobra command in commands() and read here, which is what the SDK's tree
// expects: it parses the module's own arguments with that command's flag set
// before the handler runs.
type resourceServerFlags struct {
	identifier  string
	description string
	permissions []string
}

// resourceServersCreate answers "wso2 identity resource-servers create".
//
// Two of ThunderID's rules are checked here rather than left to the
// deployment, because both are answered late and in terms that name something
// the caller never wrote. An identifier that is not an absolute URI is
// accepted at creation and refused much later, when a token exchange asks for
// it as an RFC 8707 resource indicator. A permission carrying the delimiter is
// refused by naming a delimiter the caller never chose.
func resourceServersCreate(command *cobra.Command, flags *resourceServerFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		return createResourceServer(ctx, request, command.Flags().Args(), *flags)
	}
}

func createResourceServer(
	ctx context.Context, request module.Request, positional []string, flags resourceServerFlags,
) (result.Result, error) {
	name, err := resourceServerName(positional, flags)
	if err != nil {
		return result.Result{}, err
	}
	client, err := clientFor(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	body := map[string]any{"name": name, "identifier": flags.identifier}
	if flags.description != "" {
		body["description"] = flags.description
	}
	var created resourceServer
	if err := client.Post(ctx, "/resource-servers", body, &created); err != nil {
		return result.Result{}, callFailed(err, "create the resource server", request.Context.Endpoint)
	}
	report := result.New(ResourceServerSchema).
		With("id", "ID", created.ID).
		With("name", "Name", created.Name).
		With("identifier", "Identifier", created.Identifier)

	// Permissions are a sub-resource, so each is a call of its own. A failure
	// after the resource server exists is reported as what it is rather than
	// rolled back: the deployment has no transaction to roll back into, and a
	// silent delete would destroy something the operator can see.
	granted := make([]string, 0, len(flags.permissions))
	for _, handle := range flags.permissions {
		permission := map[string]any{"name": handle, "handle": handle}
		if err := client.Post(ctx, "/resource-servers/"+created.ID+"/resources", permission, nil); err != nil {
			return report.
				With("permissions", "Permissions", strings.Join(granted, " ")).
				With(NextField, "Next", "The resource server was created and the permission "+
					handle+" was refused. Add it with wso2 account resource-servers create "+
					"--permission, or in the deployment's console."), nil
		}
		granted = append(granted, handle)
	}
	if len(granted) > 0 {
		report = report.With("permissions", "Permissions", strings.Join(granted, " "))
	}
	return report.With(NextField, "Next",
		"Record it on an account with wso2 account add-product <account> <namespace> "+
			"--endpoint <url> --audience "+created.Identifier+"."), nil
}

// resourceServerName proves the command line names exactly one resource
// server and that what it names can actually be created.
//
// Two of ThunderID's rules are checked here rather than left to the
// deployment, because both are answered late and in terms that name something
// the caller never wrote.
func resourceServerName(positional []string, flags resourceServerFlags) (string, error) {
	switch {
	case len(positional) == 0:
		return "", usageProblem("wso2 account resource-servers create needs a name")
	case len(positional) > 1:
		return "", usageProblem("this command takes one name, and was given " +
			strconv.Itoa(len(positional)))
	}
	if !absoluteURI(flags.identifier) {
		return "", usageProblem(
			"--identifier must be an absolute URI: an exchange asks for it as a resource indicator, " +
				"and a deployment refuses anything else")
	}
	for _, handle := range flags.permissions {
		if strings.Contains(handle, defaultDelimiter) {
			return "", usageProblem(
				"a permission cannot contain the delimiter " + defaultDelimiter + ": the deployment " +
					"composes a permission name with it, so " + handle + " could never be granted")
		}
	}
	return positional[0], nil
}

// absoluteURI reports whether value is a URI with a scheme and no fragment,
// which is what RFC 8707 requires of a resource indicator.
func absoluteURI(value string) bool {
	parsed, err := url.Parse(value)
	return err == nil && parsed.Scheme != "" && parsed.Host != "" && parsed.Fragment == ""
}
