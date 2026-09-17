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
	"os"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"go.yaml.in/yaml/v3"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/result"
)

// The semantic shapes a created API and a deployment take.
const (
	ApiSchema        = "api.restApi/v1"
	DeploymentSchema = "api.deployment/v1"
)

// gatewayRestApiAPIVersion and gatewayRestApiKind are the only values
// "wso2 api apis create -f" accepts, the same CR the gateway itself reads.
// See gateway/hello-api.yaml.
const (
	gatewayRestApiAPIVersion = "gateway.api-platform.wso2.com/v1"
	gatewayRestApiKind       = "RestApi"
)

// createRestAPISpecFields are the spec.* members this command knows how to
// carry onto platform-api's CreateRESTAPIRequest. A field outside this set is
// refused by name rather than silently dropped, because a field this command
// pretends to accept and then discards is a worse failure than one it never
// claimed to support.
var createRestAPISpecFields = map[string]bool{
	"displayName":       true,
	"description":       true,
	"context":           true,
	"version":           true,
	"upstream":          true,
	"policies":          true,
	"operations":        true,
	"subscriptionPlans": true,
}

// upstreamDefinitionFields are UpstreamDefinition's own members.
var upstreamDefinitionFields = map[string]bool{"url": true, "ref": true, "auth": true}

// upstreamAuthFields are UpstreamAuth's own members.
var upstreamAuthFields = map[string]bool{"type": true, "header": true, "value": true}

// policyFields are Policy's own members. params is opaque per-policy
// configuration (additionalProperties: true on platform-api's own schema), so
// nothing under it is checked.
var policyFields = map[string]bool{"name": true, "version": true, "params": true, "executionCondition": true}

// operationRequestFields are OperationRequest's own members, the ones the flat
// {method, path} entries in a gateway RestApi document's operations carry.
var operationRequestFields = map[string]bool{"method": true, "path": true, "policies": true}

// gatewayRestApiTopLevelFields are the members of the gateway RestApi document
// itself. A member outside this set is refused the same way an unmapped spec
// member is, rather than silently ignored.
var gatewayRestApiTopLevelFields = map[string]bool{
	"apiVersion": true, "kind": true, "metadata": true, "spec": true,
}

// gatewayRestApiMetadataFields are metadata's own members. This command reads
// only metadata.name; anything else is refused the same way.
var gatewayRestApiMetadataFields = map[string]bool{"name": true}

// apisCreateFlags are what "wso2 api apis create" was asked for.
type apisCreateFlags struct {
	file    string
	project string
}

// apisCreate answers "wso2 api apis create -f <file> --project <id>".
func apisCreate(command *cobra.Command, flags *apisCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		if len(command.Flags().Args()) != 0 {
			return result.Result{}, usageProblem(
				"wso2 api apis create takes no positional arguments",
				"Run wso2 api apis create -f <file> --project <id>.")
		}
		if flags.project == "" {
			return result.Result{}, usageProblem(
				"wso2 api apis create needs the project to create the API in",
				"Run wso2 api apis create -f <file> --project <id>. wso2 api projects list shows "+
					"the ids.")
		}
		if flags.file == "" {
			return result.Result{}, usageProblem(
				"wso2 api apis create needs the RestApi file to read",
				"Run wso2 api apis create -f <file> --project <id>.")
		}
		raw, err := os.ReadFile(flags.file)
		if err != nil {
			return result.Result{}, usageProblem(
				"the file at "+flags.file+" could not be read: "+err.Error(),
				"Check the path and try again.")
		}
		body, err := createRESTAPIRequest(raw, flags.project)
		if err != nil {
			return result.Result{}, err
		}

		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var created restAPI
		if err := client.Post(ctx, controlPlanePath+"/rest-apis", body, &created); err != nil {
			return result.Result{}, createCallFailed(err, request.Context.Endpoint, flags.file)
		}
		return result.New(ApiSchema).
			With("id", "ID", created.ID).
			With("displayName", "Name", created.DisplayName).
			With("version", "Version", created.Version).
			With("context", "Context", created.Context).
			With(NextField, "Next",
				"Run wso2 api apis deploy "+created.ID+" --gateway-id <gateway-id> to deploy it."), nil
	}
}

// createRESTAPIRequest reads a gateway RestApi document and maps it onto the
// body "POST /rest-apis" accepts.
//
// It validates the document's apiVersion and kind before looking at spec at
// all, then walks spec (and upstream, policies, and operations within it) and
// collects every member it cannot map, by path, before refusing them all at
// once: an operator who wrote spec.upstream.main.hostRewrite expecting it to
// reach the deployment is told exactly which path was not understood, never
// left to find out from a silently incomplete API. Only once nothing is left
// unmapped are the fields platform-api actually requires checked.
func createRESTAPIRequest(raw []byte, project string) (map[string]any, error) {
	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return nil, usageProblem(
			"the file is not valid YAML: "+err.Error(),
			"Fix the file and try again.")
	}
	apiVersion, _ := doc["apiVersion"].(string)
	kind, _ := doc["kind"].(string)
	if apiVersion != gatewayRestApiAPIVersion || kind != gatewayRestApiKind {
		return nil, usageProblem(
			fmt.Sprintf("the file names apiVersion %q and kind %q, but wso2 api apis create only "+
				"accepts apiVersion %q and kind %q", apiVersion, kind,
				gatewayRestApiAPIVersion, gatewayRestApiKind),
			"Pass a gateway RestApi document, such as gateway/hello-api.yaml.")
	}

	var unmapped []string
	for key := range doc {
		if !gatewayRestApiTopLevelFields[key] {
			unmapped = append(unmapped, key)
		}
	}

	var name string
	if metadataRaw, present := doc["metadata"]; present {
		metadata, ok := metadataRaw.(map[string]any)
		if !ok {
			return nil, usageProblem("metadata must be an object", "")
		}
		for key, value := range metadata {
			if key != "name" {
				if !gatewayRestApiMetadataFields[key] {
					unmapped = append(unmapped, "metadata."+key)
				}
				continue
			}
			nameString, ok := value.(string)
			if !ok {
				return nil, usageProblem("metadata.name must be a string", "")
			}
			name = nameString
		}
	}

	spec, ok := doc["spec"].(map[string]any)
	if !ok || spec == nil {
		return nil, usageProblem(
			"the file declares no spec",
			"A RestApi document needs a spec carrying at least displayName, context, version, "+
				"and upstream.")
	}

	for key := range spec {
		if !createRestAPISpecFields[key] {
			unmapped = append(unmapped, "spec."+key)
		}
	}
	if upstream, present := spec["upstream"]; present {
		if _, ok := upstream.(map[string]any); ok {
			// A non-object upstream is left to requiredUpstream below, which
			// gives it a message that says what shape is expected rather
			// than treating it as an unmapped field.
			unmapped = append(unmapped, validateUpstream("spec.upstream", upstream)...)
		}
	}
	if policies, present := spec["policies"]; present {
		unmapped = append(unmapped, validatePolicyList("spec.policies", policies)...)
	}
	var operations []map[string]any
	if raw, present := spec["operations"]; present {
		mapped, unmappedOperations, err := validateOperations("spec.operations", raw)
		if err != nil {
			return nil, err
		}
		operations = mapped
		unmapped = append(unmapped, unmappedOperations...)
	}
	if len(unmapped) > 0 {
		sort.Strings(unmapped)
		return nil, usageProblem(
			"this file names "+strings.Join(unmapped, ", ")+
				", which platform-api's REST API schema has no field for",
			"Remove it from the file, or map it by hand onto a field CreateRESTAPIRequest actually "+
				"defines.")
	}

	displayName, err := requiredSpecString(spec, "displayName")
	if err != nil {
		return nil, err
	}
	apiContext, err := requiredSpecString(spec, "context")
	if err != nil {
		return nil, err
	}
	version, err := requiredSpecString(spec, "version")
	if err != nil {
		return nil, err
	}
	upstream, err := requiredUpstream(spec)
	if err != nil {
		return nil, err
	}

	body := map[string]any{
		"displayName": displayName,
		"context":     apiContext,
		"version":     version,
		"projectId":   project,
		"upstream":    upstream,
	}
	if name != "" {
		body["id"] = name
	}
	if description, present := spec["description"]; present {
		body["description"] = description
	}
	if policies, present := spec["policies"]; present {
		body["policies"] = policies
	}
	if operations != nil {
		body["operations"] = operations
	}
	if plans, present := spec["subscriptionPlans"]; present {
		body["subscriptionPlans"] = plans
	}
	return body, nil
}

// requiredSpecString reads a required string member of spec, refusing a
// missing or empty value by name and a non-string one (a bare version number
// such as 1.0 decodes as a float, not a string) by telling the file's author
// to quote it.
func requiredSpecString(spec map[string]any, field string) (string, error) {
	raw, present := spec[field]
	if !present || raw == nil {
		return "", usageProblem(
			"the file's spec has no "+field,
			"Set spec."+field+" in the file.")
	}
	value, ok := raw.(string)
	if !ok {
		return "", usageProblem(
			fmt.Sprintf("spec.%s must be a string, and %v is not one", field, raw),
			"Quote spec."+field+" in the file, for example "+field+`: "1.0".`)
	}
	if strings.TrimSpace(value) == "" {
		return "", usageProblem(
			"spec."+field+" cannot be empty",
			"Set spec."+field+" in the file.")
	}
	return value, nil
}

// requiredUpstream reads spec.upstream, refusing one that is missing, is not
// an object, or names no main endpoint — Upstream.main is a required member
// of platform-api's own schema.
func requiredUpstream(spec map[string]any) (map[string]any, error) {
	raw, present := spec["upstream"]
	if !present {
		return nil, usageProblem(
			"the file's spec has no upstream",
			"Set spec.upstream.main.url (or spec.upstream.main.ref) in the file.")
	}
	upstream, ok := raw.(map[string]any)
	if !ok {
		return nil, usageProblem(
			"spec.upstream must be an object",
			"Set spec.upstream.main.url (or spec.upstream.main.ref) in the file.")
	}
	mainRaw, present := upstream["main"]
	if !present {
		return nil, usageProblem(
			"spec.upstream has no main",
			"Set spec.upstream.main.url (or spec.upstream.main.ref) in the file.")
	}
	if _, ok := mainRaw.(map[string]any); !ok {
		return nil, usageProblem(
			"spec.upstream.main must be a map with url or ref",
			"Set spec.upstream.main.url (or spec.upstream.main.ref) in the file, for example "+
				"upstream: { main: { url: http://backend:8080 } }.")
	}
	return upstream, nil
}

// validateUpstream allows only Upstream's own members, main and sandbox, and
// walks into each as an UpstreamDefinition.
func validateUpstream(path string, raw any) []string {
	object, ok := raw.(map[string]any)
	if !ok {
		return []string{path}
	}
	var unmapped []string
	for key, value := range object {
		switch key {
		case "main", "sandbox":
			// A main or sandbox that is not itself a map is a shape problem,
			// not an unmapped-field one; requiredUpstream gives spec.upstream.main
			// its own message for that, and sandbox is not required so
			// nothing else checks its shape here.
			if _, ok := value.(map[string]any); ok {
				unmapped = append(unmapped, validateUpstreamDefinition(path+"."+key, value)...)
			}
		default:
			unmapped = append(unmapped, path+"."+key)
		}
	}
	return unmapped
}

// validateUpstreamDefinition allows only UpstreamDefinition's own members —
// url, ref, and auth — and walks into auth as an UpstreamAuth.
func validateUpstreamDefinition(path string, raw any) []string {
	object, ok := raw.(map[string]any)
	if !ok {
		return []string{path}
	}
	var unmapped []string
	for key, value := range object {
		if !upstreamDefinitionFields[key] {
			unmapped = append(unmapped, path+"."+key)
			continue
		}
		if key == "auth" {
			unmapped = append(unmapped, validateUpstreamAuth(path+".auth", value)...)
		}
	}
	return unmapped
}

// validateUpstreamAuth allows only UpstreamAuth's own members.
func validateUpstreamAuth(path string, raw any) []string {
	object, ok := raw.(map[string]any)
	if !ok {
		return []string{path}
	}
	var unmapped []string
	for key := range object {
		if !upstreamAuthFields[key] {
			unmapped = append(unmapped, path+"."+key)
		}
	}
	return unmapped
}

// validatePolicyList allows only Policy's own members in every entry of a
// policies list, by path, e.g. spec.policies[0].foo.
func validatePolicyList(path string, raw any) []string {
	list, ok := raw.([]any)
	if !ok {
		return []string{path}
	}
	var unmapped []string
	for index, item := range list {
		entryPath := fmt.Sprintf("%s[%d]", path, index)
		object, ok := item.(map[string]any)
		if !ok {
			unmapped = append(unmapped, entryPath)
			continue
		}
		for key := range object {
			if !policyFields[key] {
				unmapped = append(unmapped, entryPath+"."+key)
			}
		}
	}
	return unmapped
}

// validateOperations maps the flat {method, path, policies} the gateway
// RestApi document carries onto platform-api's Operation shape, which nests
// method and path under request, and carries policies there too. An entry
// carrying a member outside operationRequestFields is refused by path rather
// than dropped, and an entry that is not an object is refused outright rather
// than skipped, so an operation this command cannot understand is never
// silently missing from the API it creates.
func validateOperations(path string, raw any) ([]map[string]any, []string, error) {
	list, ok := raw.([]any)
	if !ok {
		return nil, nil, usageProblem(path+" must be a list of operations", "")
	}
	var unmapped []string
	mapped := make([]map[string]any, 0, len(list))
	for index, item := range list {
		entryPath := fmt.Sprintf("%s[%d]", path, index)
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, nil, usageProblem(
				entryPath+" must be an object naming method and path",
				"")
		}
		for key := range entry {
			if !operationRequestFields[key] {
				unmapped = append(unmapped, entryPath+"."+key)
			}
		}
		operationRequest := map[string]any{
			"method": entry["method"],
			"path":   entry["path"],
		}
		if policies, present := entry["policies"]; present {
			operationRequest["policies"] = policies
			unmapped = append(unmapped, validatePolicyList(entryPath+".policies", policies)...)
		}
		mapped = append(mapped, map[string]any{"request": operationRequest})
	}
	return mapped, unmapped, nil
}

// apisDeployFlags are what "wso2 api apis deploy" was asked for.
type apisDeployFlags struct {
	gatewayID string
}

// deployment is platform-api's DeploymentResponse.
type deployment struct {
	DeploymentID string `json:"deploymentId"`
	Name         string `json:"name"`
	GatewayID    string `json:"gatewayId"`
	Status       string `json:"status"`
}

// apisDeploy answers "wso2 api apis deploy <api-id> --gateway-id <gateway-id>".
//
// This posts the deployment alone. platform-api's own DeployAPI operation
// associates the API with the target gateway itself when it is not already
// associated, and POST /rest-apis/{id}/gateways takes an array of
// AddGatewayToRESTAPIRequest (AddGatewaysToAPI) rather than one — a step this
// command would have to keep duplicating DeployAPI's own association logic to
// get right, for no benefit over letting DeployAPI do it once.
func apisDeploy(command *cobra.Command, flags *apisDeployFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		apiID, err := exactlyOneArgument(command.Flags().Args(),
			"wso2 api apis deploy needs the id of the API to deploy",
			"Run wso2 api apis deploy <api-id> --gateway-id <gateway-id>. wso2 api apis list "+
				"--project <id> shows the ids.")
		if err != nil {
			return result.Result{}, err
		}
		if flags.gatewayID == "" {
			return result.Result{}, usageProblem(
				"wso2 api apis deploy needs the gateway to deploy to",
				"Run wso2 api apis deploy "+apiID+" --gateway-id <gateway-id>.")
		}

		client, err := controlPlane(ctx, request)
		if err != nil {
			return result.Result{}, err
		}

		body := map[string]any{
			"name":      apiID + "-" + flags.gatewayID,
			"base":      "current",
			"gatewayId": flags.gatewayID,
		}
		var created deployment
		path := controlPlanePath + "/rest-apis/" + url.PathEscape(apiID) + "/deployments"
		if err := client.Post(ctx, path, body, &created); err != nil {
			return result.Result{}, callFailed(err, "deploy the API", request.Context.Endpoint)
		}
		return result.New(DeploymentSchema).
			With("deploymentId", "Deployment ID", created.DeploymentID).
			With("apiId", "API", apiID).
			With("gatewayId", "Gateway", flags.gatewayID).
			With("status", "Status", created.Status).
			With(NextField, "Next",
				"Run wso2 api gateway apis list to see it once the gateway has deployed it."), nil
	}
}
