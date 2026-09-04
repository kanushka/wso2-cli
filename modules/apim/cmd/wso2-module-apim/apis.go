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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/apim/internal/apim"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	APIsSchema    = "apim.apis/v1"
	APISchema     = "apim.api/v1"
	DeploySchema  = "apim.deployment/v1"
	PublishSchema = "apim.lifecycle/v1"
)

// deployWait bounds how long deploy waits for the gateway to report the
// revision live; the gateway answers 404 until then.
var (
	deployWait     = 60 * time.Second
	deployInterval = 2 * time.Second
)

type api struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Version         string `json:"version"`
	Context         string `json:"context"`
	LifeCycleStatus string `json:"lifeCycleStatus"`
}

type apiList struct {
	Count int   `json:"count"`
	List  []api `json:"list"`
}

type apiImportFlags struct {
	file, name, version, context, backend, policy string
}

type apiDeployFlags struct {
	gateway, vhost string
}

func apiCommands() (family, list, importCommand, deploy, publish *cobra.Command,
	importFlags *apiImportFlags, deployFlags *apiDeployFlags) {
	family = &cobra.Command{Use: "apis", Short: "Manage the APIs the publisher holds."}
	list = &cobra.Command{Use: "list", Short: "List the APIs."}
	importFlags = &apiImportFlags{}
	importCommand = &cobra.Command{
		Use:   "import --file <openapi> --name <name> --version <v> --context </path> --backend <url>",
		Short: "Create an API from an OpenAPI definition.",
	}
	f := importCommand.Flags()
	f.StringVar(&importFlags.file, "file", "", "The OpenAPI definition, YAML or JSON.")
	f.StringVar(&importFlags.name, "name", "", "The API's name.")
	f.StringVar(&importFlags.version, "version", "", "The API's version.")
	f.StringVar(&importFlags.context, "context", "", "The API's context path on the gateway, such as /mockapi.")
	f.StringVar(&importFlags.backend, "backend", "", "The backend URL the gateway forwards to.")
	f.StringVar(&importFlags.policy, "policy", "Unlimited", "The subscription throttling policy.")
	deployFlags = &apiDeployFlags{}
	deploy = &cobra.Command{
		Use:   "deploy <name/version> [--gateway Default] [--vhost localhost]",
		Short: "Create a revision and deploy it to a gateway, waiting until it is live.",
	}
	deploy.Flags().StringVar(&deployFlags.gateway, "gateway", "Default", "The gateway environment.")
	deploy.Flags().StringVar(&deployFlags.vhost, "vhost", "localhost", "The virtual host on that gateway.")
	publish = &cobra.Command{Use: "publish <name/version>", Short: "Move the API to the Published state."}
	family.AddCommand(list, importCommand, deploy, publish)
	return family, list, importCommand, deploy, publish, importFlags, deployFlags
}

func apisList(ctx context.Context, request module.Request) (result.Result, error) {
	client, err := managementClient(ctx, request, ScopeAPIView)
	if err != nil {
		return result.Result{}, err
	}
	var listed apiList
	if err := client.Get(ctx, publisherPath+"/apis", &listed); err != nil {
		return result.Result{}, apim.Problem(err, "the API listing")
	}
	names := make([]string, 0, len(listed.List))
	for _, api := range listed.List {
		names = append(names, api.Name+"/"+api.Version+" "+api.Context+" ("+api.LifeCycleStatus+")")
	}
	return result.New(APIsSchema).
		With("count", "Count", fmt.Sprintf("%d", listed.Count)).
		With("apis", "APIs", joined(names)).
		With(NextField, "Next", "Run wso2 apim apis import --file <openapi> --name <name> --version <v> "+
			"--context </path> --backend <url> to create one."), nil
}

func apisImport(flags *apiImportFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		for flag, value := range map[string]string{"--file": flags.file, "--name": flags.name,
			"--version": flags.version, "--context": flags.context, "--backend": flags.backend} {
			if value == "" {
				return result.Result{}, missingFlag("apis import", flag,
					"Pass --file, --name, --version, --context and --backend; --policy defaults to Unlimited.")
			}
		}
		definition, err := os.Open(flags.file)
		if err != nil {
			return result.Result{}, problem.New(problem.CategoryUsage, "apim.unreadable_file",
				fmt.Sprintf("the OpenAPI file %q cannot be read", flags.file)).
				WithRecovery("Pass --file with a readable OpenAPI definition.")
		}
		defer definition.Close()
		client, err := managementClient(ctx, request, ScopeAPICreate, ScopeAPIView)
		if err != nil {
			return result.Result{}, err
		}
		if existing, found, err := findAPI(ctx, client, publisherPath, flags.name, flags.version); err != nil {
			return result.Result{}, err
		} else if found {
			return apiResult(existing, false), nil
		}
		properties, _ := json.Marshal(map[string]any{
			"name": flags.name, "version": flags.version, "context": flags.context,
			"policies": []string{flags.policy},
			"endpointConfig": map[string]any{
				"endpoint_type":        "http",
				"production_endpoints": map[string]string{"url": flags.backend},
				"sandbox_endpoints":    map[string]string{"url": flags.backend},
			},
		})
		var created api
		if err := client.PostMultipart(ctx, publisherPath+"/apis/import-openapi", definition,
			filepath.Base(flags.file), map[string]string{"additionalProperties": string(properties)}, &created); err != nil {
			return result.Result{}, apim.Problem(err, "the API import")
		}
		return apiResult(created, true), nil
	}
}

func apiResult(created api, isNew bool) result.Result {
	reference := created.Name + "/" + created.Version
	return result.New(APISchema).
		With("id", "ID", created.ID).
		With("name", "Name", created.Name).
		With("version", "Version", created.Version).
		With("context", "Context", created.Context).
		With("state", "State", created.LifeCycleStatus).
		With("created", "Created", createdWord(isNew)).
		With(NextField, "Next", fmt.Sprintf("Run wso2 apim apis deploy %s to put it on the gateway.", reference))
}

func apisDeploy(command *cobra.Command, flags *apiDeployFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		reference, err := oneArgument(command, "an API as <name>/<version>")
		if err != nil {
			return result.Result{}, err
		}
		name, version, err := nameVersion(reference)
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request, ScopeAPIPublish, ScopeAPIView)
		if err != nil {
			return result.Result{}, err
		}
		found, ok, err := findAPI(ctx, client, publisherPath, name, version)
		if err != nil {
			return result.Result{}, err
		}
		if !ok {
			return result.Result{}, notFound("API", reference, "apis list")
		}
		var revision struct {
			ID string `json:"id"`
		}
		if err := client.Post(ctx, publisherPath+"/apis/"+found.ID+"/revisions",
			map[string]string{"description": "wso2 apim apis deploy"}, &revision); err != nil {
			return result.Result{}, apim.Problem(err, "the revision creation")
		}
		var deployed []map[string]any
		if err := client.Post(ctx, publisherPath+"/apis/"+found.ID+"/deploy-revision?revisionId="+url.QueryEscape(revision.ID),
			[]map[string]any{{"name": flags.gateway, "vhost": flags.vhost, "displayOnDevportal": true}}, &deployed); err != nil {
			return result.Result{}, apim.Problem(err, "the revision deployment")
		}
		status, err := awaitDeployment(ctx, client, found.ID, flags.gateway)
		if err != nil {
			return result.Result{}, err
		}
		return result.New(DeploySchema).
			With("id", "ID", found.ID).
			With("api", "API", reference).
			With("revision", "Revision", revision.ID).
			With("gateway", "Gateway", flags.gateway).
			With("status", "Status", status).
			With(NextField, "Next", fmt.Sprintf("Run wso2 apim apis publish %s to make it subscribable.", reference)), nil
	}
}

// awaitDeployment polls until the gateway reports the revision live.
func awaitDeployment(ctx context.Context, client *apim.Client, id, gateway string) (string, error) {
	deadline := time.Now().Add(deployWait)
	for {
		var deployments []struct {
			Name        string `json:"name"`
			SuccessTime int64  `json:"successDeployedTime"`
		}
		if err := client.Get(ctx, publisherPath+"/apis/"+id+"/deployments", &deployments); err != nil {
			return "", apim.Problem(err, "the deployment status")
		}
		for _, deployment := range deployments {
			if deployment.Name == gateway && deployment.SuccessTime > 0 {
				return "live", nil
			}
		}
		if time.Now().After(deadline) {
			return "pending", nil
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(deployInterval):
		}
	}
}

func apisPublish(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		reference, err := oneArgument(command, "an API as <name>/<version>")
		if err != nil {
			return result.Result{}, err
		}
		name, version, err := nameVersion(reference)
		if err != nil {
			return result.Result{}, err
		}
		client, err := managementClient(ctx, request, ScopeAPIPublish, ScopeAPIView)
		if err != nil {
			return result.Result{}, err
		}
		found, ok, err := findAPI(ctx, client, publisherPath, name, version)
		if err != nil {
			return result.Result{}, err
		}
		if !ok {
			return result.Result{}, notFound("API", reference, "apis list")
		}
		state := found.LifeCycleStatus
		changed := false
		if state != "PUBLISHED" {
			var answer struct {
				LifecycleState struct {
					State string `json:"state"`
				} `json:"lifecycleState"`
			}
			if err := client.Post(ctx, publisherPath+"/apis/change-lifecycle?apiId="+url.QueryEscape(found.ID)+
				"&action=Publish", nil, &answer); err != nil {
				return result.Result{}, apim.Problem(err, "the lifecycle change")
			}
			state, changed = "PUBLISHED", true
		}
		return result.New(PublishSchema).
			With("id", "ID", found.ID).
			With("api", "API", reference).
			With("state", "State", state).
			With("changed", "Changed", createdWord(changed)).
			With(NextField, "Next", fmt.Sprintf("Run wso2 apim apps create <name>, then wso2 apim apps subscribe <name> %s.",
				reference)), nil
	}
}

// findAPI looks an API up by name and version on a plane.
func findAPI(ctx context.Context, client *apim.Client, plane, name, version string) (api, bool, error) {
	var listed apiList
	if err := client.Get(ctx, plane+"/apis?query="+url.QueryEscape("name:"+name), &listed); err != nil {
		return api{}, false, apim.Problem(err, "the API lookup")
	}
	for _, candidate := range listed.List {
		if candidate.Name == name && candidate.Version == version {
			return candidate, true, nil
		}
	}
	return api{}, false, nil
}
