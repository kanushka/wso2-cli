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
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	KeyManagersSchema = "apim.key-managers/v1"
	KeyManagerSchema  = "apim.key-manager/v1"
)

type keyManager struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Enabled bool   `json:"enabled"`
}

type keyManagerList struct {
	Count int          `json:"count"`
	List  []keyManager `json:"list"`
}

type keyManagerFlags struct {
	wellKnown, jwks, tokenEndpoint, revokeEndpoint, consumerKeyClaim, scopesClaim string
}

func keyManagerCommands() (family, list, add *cobra.Command, flags *keyManagerFlags) {
	flags = &keyManagerFlags{}
	family = &cobra.Command{Use: "key-managers", Short: "Manage the issuers the gateway accepts tokens from."}
	list = &cobra.Command{Use: "list", Short: "List the key managers."}
	add = &cobra.Command{
		Use:   "add <name> --well-known <issuer> [--jwks <url>] [--token-endpoint <url>] [--revoke-endpoint <url>]",
		Short: "Register an external issuer as a custom key manager that validates its JWTs.",
		Long: "Reads the issuer's OpenID discovery document for the JWKS, token and revocation " +
			"endpoints, each overridable, because the gateway may see the issuer at another address " +
			"than this machine does (a container reaching the host, say).",
	}
	f := add.Flags()
	f.StringVar(&flags.wellKnown, "well-known", "", "The issuer whose discovery document to read.")
	f.StringVar(&flags.jwks, "jwks", "", "The JWKS URL as the gateway must reach it.")
	f.StringVar(&flags.tokenEndpoint, "token-endpoint", "", "The token endpoint, if not the discovered one.")
	f.StringVar(&flags.revokeEndpoint, "revoke-endpoint", "", "The revocation endpoint, if not the discovered one.")
	f.StringVar(&flags.consumerKeyClaim, "consumer-key-claim", "client_id", "The JWT claim carrying the client id.")
	f.StringVar(&flags.scopesClaim, "scopes-claim", "scope", "The JWT claim carrying the scopes.")
	family.AddCommand(list, add)
	return family, list, add, flags
}

func keyManagersList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request)
	if err != nil {
		return result.Result{}, err
	}
	var listed keyManagerList
	if err := client.Get(ctx, adminPath+"/key-managers", &listed); err != nil {
		return result.Result{}, apim.Problem(err, "the key manager listing")
	}
	names := make([]string, 0, len(listed.List))
	for _, manager := range listed.List {
		names = append(names, manager.Name+" ("+manager.Type+")")
	}
	return result.New(KeyManagersSchema).
		With("count", "Count", fmt.Sprintf("%d", listed.Count)).
		With("keyManagers", "Key managers", joined(names)).
		With(NextField, "Next", "Run wso2 apim key-managers add <name> --well-known <issuer> to accept another issuer's tokens."), nil
}

func keyManagersAdd(command *cobra.Command, flags *keyManagerFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := oneArgument(command, "a key manager name")
		if err != nil {
			return result.Result{}, err
		}
		if flags.wellKnown == "" {
			return result.Result{}, missingFlag("key-managers add", "--well-known",
				"Pass --well-known <issuer>; its discovery document supplies the endpoints.")
		}
		client, err := managementClient(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listed keyManagerList
		if err := client.Get(ctx, adminPath+"/key-managers", &listed); err != nil {
			return result.Result{}, apim.Problem(err, "the key manager listing")
		}
		for _, manager := range listed.List {
			if manager.Name == name {
				return keyManagerResult(manager, name, false), nil
			}
		}
		issuer := strings.TrimRight(flags.wellKnown, "/")
		var discovered struct {
			Issuer     string `json:"issuer"`
			JWKS       string `json:"jwks_uri"`
			Token      string `json:"token_endpoint"`
			Revocation string `json:"revocation_endpoint"`
		}
		if err := apim.New(issuer, "").Get(ctx, "/.well-known/openid-configuration", &discovered); err != nil {
			return result.Result{}, apim.Problem(err, "reading the issuer's discovery document")
		}
		// Two key managers with one issuer leave the gateway unable to tell
		// which validates a token, and it then rejects every token from that
		// issuer (measured). The listing carries no issuer, so each custom
		// key manager is read in full.
		for _, manager := range listed.List {
			if manager.Type != "CustomKeyManager" {
				continue
			}
			var full struct {
				Issuer string `json:"issuer"`
			}
			if err := client.Get(ctx, adminPath+"/key-managers/"+manager.ID, &full); err != nil {
				return result.Result{}, apim.Problem(err, "reading the key manager "+manager.Name)
			}
			if strings.TrimRight(full.Issuer, "/") == firstOf(strings.TrimRight(discovered.Issuer, "/"), issuer) {
				return result.Result{}, problem.New(problem.CategoryUsage, "apim.issuer_registered",
					fmt.Sprintf("the key manager %q already validates tokens from %s", manager.Name, full.Issuer)).
					WithRecovery(fmt.Sprintf("Use --key-manager %s on wso2 apim apps map-keys. A second key manager "+
						"for the same issuer makes the gateway reject every token from it.", manager.Name))
			}
		}
		jwks, token, revoke := firstOf(flags.jwks, discovered.JWKS), firstOf(flags.tokenEndpoint, discovered.Token),
			firstOf(flags.revokeEndpoint, discovered.Revocation, issuer+"/oauth2/revoke")
		body := map[string]any{
			"name": name, "displayName": name, "type": "CustomKeyManager",
			"description":   "Registered by wso2 apim key-managers add",
			"issuer":        firstOf(discovered.Issuer, issuer),
			"certificates":  map[string]string{"type": "JWKS", "value": jwks},
			"tokenEndpoint": token, "displayTokenEndpoint": token,
			"revokeEndpoint": revoke, "displayRevokeEndpoint": revoke,
			"availableGrantTypes":   []string{"client_credentials", "authorization_code", "refresh_token"},
			"enableTokenGeneration": false, "enableTokenEncryption": false, "enableTokenHashing": false,
			"enableMapOAuthConsumerApps": true, "enableOAuthAppCreation": false, "enableSelfValidationJWT": true,
			"consumerKeyClaim": flags.consumerKeyClaim, "scopesClaim": flags.scopesClaim,
			"tokenValidation": []map[string]any{{"enable": true, "type": "JWT", "value": map[string]any{"body": map[string]any{}}}},
			"enabled":         true, "tokenType": "DIRECT",
			"permissions": map[string]any{"permissionType": "PUBLIC", "roles": []string{}},
		}
		var created keyManager
		if err := client.Post(ctx, adminPath+"/key-managers", body, &created); err != nil {
			return result.Result{}, apim.Problem(err, "the key manager registration")
		}
		return keyManagerResult(created, name, true), nil
	}
}

func keyManagerResult(manager keyManager, name string, created bool) result.Result {
	return result.New(KeyManagerSchema).
		With("id", "ID", manager.ID).
		With("name", "Name", name).
		With("type", "Type", manager.Type).
		With("created", "Created", createdWord(created)).
		With(NextField, "Next", fmt.Sprintf("Run wso2 apim apps map-keys <app> --key-manager %s --client-id <id> "+
			"to let a client of that issuer call a subscribed API.", name))
}

func firstOf(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
