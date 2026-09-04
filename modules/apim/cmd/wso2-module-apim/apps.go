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
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	AppsSchema    = "apim.apps/v1"
	AppSchema     = "apim.app/v1"
	SubSchema     = "apim.subscription/v1"
	KeysSchema    = "apim.keys/v1"
	MapKeysSchema = "apim.key-mapping/v1"
)

// mapKeysRetries covers the few seconds a new key manager takes to register.
var (
	mapKeysRetries  = 5
	mapKeysInterval = 2 * time.Second
)

type application struct {
	ID     string `json:"applicationId"`
	Name   string `json:"name"`
	Policy string `json:"throttlingPolicy"`
}

type applicationList struct {
	Count int           `json:"count"`
	List  []application `json:"list"`
}

type keyPair struct {
	KeyMappingID   string `json:"keyMappingId"`
	KeyType        string `json:"keyType"`
	KeyState       string `json:"keyState"`
	ConsumerKey    string `json:"consumerKey"`
	ConsumerSecret string `json:"consumerSecret"`
	KeyManager     string `json:"keyManager"`
	Mode           string `json:"mode"`
}

type appFlags struct {
	policy      string
	keyManager  string
	grants      []string
	clientID    string
	keyType     string
	mapManager  string
	mapClientID string
	mapKeyType  string
}

func appCommands() (family, list, create, subscribe, keys, mapKeys *cobra.Command, flags *appFlags) {
	flags = &appFlags{}
	family = &cobra.Command{Use: "apps", Short: "Manage devportal applications, subscriptions and keys."}
	list = &cobra.Command{Use: "list", Short: "List the applications."}
	create = &cobra.Command{Use: "create <name> [--policy Unlimited]", Short: "Create an application."}
	create.Flags().StringVar(&flags.policy, "policy", "Unlimited", "The application throttling policy.")
	subscribe = &cobra.Command{Use: "subscribe <app> <name/version> [--policy Unlimited]", Short: "Subscribe an application to an API."}
	subscribe.Flags().StringVar(&flags.policy, "policy", "Unlimited", "The subscription throttling policy.")
	keys = &cobra.Command{Use: "keys <app> [--key-manager \"Resident Key Manager\"]",
		Short: "Generate production keys on a key manager and verify them with one token request."}
	keys.Flags().StringVar(&flags.keyManager, "key-manager", "Resident Key Manager", "The key manager to generate on.")
	keys.Flags().StringArrayVar(&flags.grants, "grant", []string{"client_credentials"}, "A grant type to allow; repeat for each.")
	keys.Flags().StringVar(&flags.keyType, "key-type", "PRODUCTION", "PRODUCTION or SANDBOX.")
	mapKeys = &cobra.Command{Use: "map-keys <app> --key-manager <name> --client-id <id>",
		Short: "Map a client registered elsewhere onto an application (out-of-band keys)."}
	mapKeys.Flags().StringVar(&flags.mapManager, "key-manager", "", "The key manager that issued the client.")
	mapKeys.Flags().StringVar(&flags.mapClientID, "client-id", "", "The client id to map.")
	mapKeys.Flags().StringVar(&flags.mapKeyType, "key-type", "PRODUCTION", "PRODUCTION or SANDBOX.")
	family.AddCommand(list, create, subscribe, keys, mapKeys)
	return family, list, create, subscribe, keys, mapKeys, flags
}

func appsList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request, ScopeSubscribe)
	if err != nil {
		return result.Result{}, err
	}
	var listed applicationList
	if err := client.Get(ctx, devportalPath+"/applications", &listed); err != nil {
		return result.Result{}, apim.Problem(err, "the application listing")
	}
	names := make([]string, 0, len(listed.List))
	for _, app := range listed.List {
		names = append(names, app.Name)
	}
	return result.New(AppsSchema).
		With("count", "Count", fmt.Sprintf("%d", listed.Count)).
		With("apps", "Applications", joined(names)).
		With(NextField, "Next", "Run wso2 apim apps create <name> to add one."), nil
}

func appsCreate(command *cobra.Command, flags *appFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := oneArgument(command, "an application name")
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request, ScopeAppManage, ScopeSubscribe)
		if err != nil {
			return result.Result{}, err
		}
		app, found, err := findApplication(ctx, client, name)
		if err != nil {
			return result.Result{}, err
		}
		if !found {
			if err := client.Post(ctx, devportalPath+"/applications",
				map[string]any{"name": name, "throttlingPolicy": flags.policy, "tokenType": "JWT"}, &app); err != nil {
				return result.Result{}, apim.Problem(err, "the application creation")
			}
		}
		return result.New(AppSchema).
			With("id", "ID", app.ID).
			With("name", "Name", name).
			With("created", "Created", createdWord(!found)).
			With(NextField, "Next", fmt.Sprintf("Run wso2 apim apps subscribe %s <name/version>.", name)), nil
	}
}

func appsSubscribe(command *cobra.Command, flags *appFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		appName, reference, err := twoArguments(command, "an application name and an API as <name>/<version>")
		if err != nil {
			return result.Result{}, err
		}
		name, version, err := nameVersion(reference)
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request, ScopeSubscribe)
		if err != nil {
			return result.Result{}, err
		}
		app, found, err := findApplication(ctx, client, appName)
		if err != nil {
			return result.Result{}, err
		}
		if !found {
			return result.Result{}, notFound("application", appName, "apps list")
		}
		target, ok, err := findAPI(ctx, client, devportalPath, name, version)
		if err != nil {
			return result.Result{}, err
		}
		if !ok {
			return result.Result{}, notFound("published API", reference, "apis list")
		}
		var subscription struct {
			ID     string `json:"subscriptionId"`
			Status string `json:"status"`
		}
		created := true
		err = client.Post(ctx, devportalPath+"/subscriptions",
			map[string]any{"applicationId": app.ID, "apiId": target.ID, "throttlingPolicy": flags.policy}, &subscription)
		if err != nil {
			var refusal *apim.Refusal
			if !apim.IsRefusalContaining(err, &refusal, "already exists") {
				return result.Result{}, apim.Problem(err, "the subscription")
			}
			created, subscription.Status = false, "UNBLOCKED"
		}
		return result.New(SubSchema).
			With("app", "Application", appName).
			With("api", "API", reference).
			With("status", "Status", subscription.Status).
			With("created", "Created", createdWord(created)).
			With(NextField, "Next", fmt.Sprintf("Run wso2 apim apps keys %s for keys on the resident key manager, "+
				"or wso2 apim apps map-keys %s --key-manager <name> --client-id <id> for a client registered elsewhere.",
				appName, appName)), nil
	}
}

func appsKeys(command *cobra.Command, flags *appFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		appName, err := oneArgument(command, "an application name")
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request, ScopeAppManage, ScopeSubscribe)
		if err != nil {
			return result.Result{}, err
		}
		app, found, err := findApplication(ctx, client, appName)
		if err != nil {
			return result.Result{}, err
		}
		if !found {
			return result.Result{}, notFound("application", appName, "apps list")
		}
		var pair keyPair
		if err := client.Post(ctx, devportalPath+"/applications/"+app.ID+"/generate-keys", map[string]any{
			"keyType": flags.keyType, "grantTypesToBeSupported": flags.grants, "validityTime": 3600,
			"keyManager": flags.keyManager, "scopes": []string{"default"},
		}, &pair); err != nil {
			return result.Result{}, apim.Problem(err, "the key generation")
		}
		// Verify the key with one token request: a mapping the key manager did
		// not honour was observed once, and a user finds out here, not at the
		// gateway.
		verified := "true"
		var token struct {
			AccessToken string `json:"access_token"`
		}
		if err := client.PostForm(ctx, "/oauth2/token", url.Values{"grant_type": {"client_credentials"}},
			pair.ConsumerKey, pair.ConsumerSecret, &token); err != nil || token.AccessToken == "" {
			return result.Result{}, problem.New(problem.CategoryProductService, "apim.refused",
				fmt.Sprintf("the key manager did not honour the key it just issued for %q", appName)).
				WithRecovery("Delete the application's key mapping in the developer portal and run wso2 apim apps keys again.")
		}
		return result.New(KeysSchema).
			With("app", "Application", appName).
			With("keyManager", "Key manager", pair.KeyManager).
			With("consumerKey", "Consumer key", pair.ConsumerKey).
			With("consumerSecret", "Consumer secret", pair.ConsumerSecret).
			With("verified", "Verified", verified).
			With(NextField, "Next", "Run wso2 apim gateway invoke </context/version/path> --context <caller> "+
				"to call the API through the gateway."), nil
	}
}

func appsMapKeys(command *cobra.Command, flags *appFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		appName, err := oneArgument(command, "an application name")
		if err != nil {
			return result.Result{}, err
		}
		if flags.mapManager == "" || flags.mapClientID == "" {
			return result.Result{}, missingFlag("apps map-keys", "--key-manager and --client-id",
				"Name the key manager that issued the client and the client id to map.")
		}
		client, err := managementClient(ctx, request, ScopeAppManage, ScopeSubscribe)
		if err != nil {
			return result.Result{}, err
		}
		app, found, err := findApplication(ctx, client, appName)
		if err != nil {
			return result.Result{}, err
		}
		if !found {
			return result.Result{}, notFound("application", appName, "apps list")
		}
		body := map[string]any{"consumerKey": flags.mapClientID, "consumerSecret": "not-held-here",
			"keyManager": flags.mapManager, "keyType": flags.mapKeyType}
		var mapped keyPair
		for attempt := 1; ; attempt++ {
			err = client.Post(ctx, devportalPath+"/applications/"+app.ID+"/map-keys", body, &mapped)
			if err == nil {
				break
			}
			var refusal *apim.Refusal
			if apim.IsRefusalContaining(err, &refusal, "Key Manager not Registered") && attempt < mapKeysRetries {
				select {
				case <-ctx.Done():
					return result.Result{}, ctx.Err()
				case <-time.After(mapKeysInterval):
				}
				continue
			}
			if apim.IsRefusalContaining(err, &refusal, "already") {
				mapped.Mode = "MAPPED (already)"
				break
			}
			return result.Result{}, apim.Problem(err, "the key mapping")
		}
		return result.New(MapKeysSchema).
			With("app", "Application", appName).
			With("keyManager", "Key manager", flags.mapManager).
			With("clientId", "Client ID", flags.mapClientID).
			With("mode", "Mode", mapped.Mode).
			With(NextField, "Next", "Run wso2 apim gateway invoke </context/version/path> --context <caller> "+
				"under an identity that logs in to that key manager's issuer."), nil
	}
}

func findApplication(ctx context.Context, client *apim.Client, name string) (application, bool, error) {
	var listed applicationList
	if err := client.Get(ctx, devportalPath+"/applications?query="+url.QueryEscape(name), &listed); err != nil {
		return application{}, false, apim.Problem(err, "the application lookup")
	}
	for _, app := range listed.List {
		if strings.EqualFold(app.Name, name) {
			return app, true, nil
		}
	}
	return application{}, false, nil
}
