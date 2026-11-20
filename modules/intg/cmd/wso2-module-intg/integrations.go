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
	"maps"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/intg/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// IntegrationsSchema identifies the integration listing.
const IntegrationsSchema = "intg.integrations/v1"

// IntegrationSchema identifies one integration, as integration show reports it.
const IntegrationSchema = "intg.integration/v1"

// CreatedIntegrationSchema identifies what integration create reports: the
// integration as created, which can carry a name the platform chose and a
// warning about a step after the create.
const CreatedIntegrationSchema = "intg.createdIntegration/v1"

// IntegrationRemovalSchema identifies an accepted integration delete.
const IntegrationRemovalSchema = "intg.integrationRemoval/v1"

// integrationTypes maps each --type to the platform integration type it creates.
// The names and the mapping are the Integration Platform's own, from the
// create_integration tool of its MCP server, so the CLI, the MCP tools and
// the console say the same thing.
var integrationTypes = map[string]string{
	"integration-as-api": "deployment/integration-as-api",
	"automation":         "cronjob/scheduled-task",
	"event-integration":  "deployment/event-integration",
	"file-integration":   "deployment/file-integration",
	"mcp-server":         "deployment/mcp-server",
}

// integrationRuntimes maps each --runtime to the build workflow that builds it,
// from the same tool.
var integrationRuntimes = map[string]string{
	"wso2-integrator":    "ballerina-buildpack-builder",
	"wso2-integrator-mi": "mi-buildpack-builder",
	"dockerfile":         "dockerfile-builder",
}

// reportedTypes maps the logical type the API reports back to the --type
// word, so a listing reads in the terms create takes. Types create does not
// offer keep a hyphenated form of their own name.
var reportedTypes = map[string]string{
	"service":          "integration-as-api",
	"automation":       "automation",
	"eventIntegration": "event-integration",
	"fileIntegration":  "file-integration",
	"mcpServer":        "mcp-server",
	"aiAgent":          "ai-agent",
	"proxy":            "proxy",
}

// reportedRuntimes maps the buildpack type the API reports to the --runtime
// word. The API reports a Dockerfile build, and any other non-buildpack one,
// as "other".
var reportedRuntimes = map[string]string{
	"BI":    "wso2-integrator",
	"MI":    "wso2-integrator-mi",
	"other": "other",
}

// integrationName is what the platform accepts as an integration's resource name:
// lowercase letters, digits and hyphens, at most 63 characters.
var integrationName = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// integration is the part of the API's Integration this module reports. id is
// the resource name every other command takes; name is the display name.
type integration struct {
	Project     string `json:"projectId"`
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"componentType"`
	Buildpack   string `json:"buildpackType"`
	Prebuilt    bool   `json:"isPrebuilt"`
	Status      string `json:"status"`
	Updated     string `json:"lastBuildDate"`
	Created     string `json:"createdAt"`
	Deleting    bool   `json:"deleting"`
	Warning     string `json:"warning"`
}

func (c integration) kind() string {
	if word, known := reportedTypes[c.Type]; known {
		return word
	}
	return orDash(c.Type)
}

func (c integration) runtime() string {
	if word, known := reportedRuntimes[c.Buildpack]; known {
		return word
	}
	if c.Prebuilt {
		return "prebuilt"
	}
	return orDash(c.Buildpack)
}

func (c integration) state() string {
	if c.Deleting {
		return "deleting"
	}
	return "active"
}

// integrationRepository is the part of the API's IntegrationRepository this
// module reports.
type integrationRepository struct {
	Provider     string `json:"gitProvider"`
	Organization string `json:"organizationApp"`
	Name         string `json:"nameApp"`
	Branch       string `json:"branch"`
	Path         string `json:"appSubPath"`
	URL          string `json:"serverUrl"`
}

func (r integrationRepository) describe() string {
	switch {
	case r.Name != "" && r.Organization != "":
		qualified := r.Organization + "/" + r.Name
		if r.Provider != "" {
			return r.Provider + ": " + qualified
		}
		return qualified
	case r.URL != "":
		return r.URL
	default:
		return "-"
	}
}

func projectIntegrationsPath(projectID string) string {
	return projectPath(projectID) + "/components"
}

func integrationPath(id string) string {
	return "/components/" + url.PathEscape(id)
}

// withProject adds projectName to a path when a project is known.
func withProject(path, projectID string) string {
	if projectID == "" {
		return path
	}
	return path + "?" + url.Values{"projectName": {projectID}}.Encode()
}

// integrationListRecovery is where to find an integration's ID.
func integrationListRecovery(projectID string) string {
	if projectID == "" {
		return "Run wso2 intg integration list to see the IDs of this organization's integrations."
	}
	return "Run wso2 intg integration list --project " + projectID + " to see the IDs of its integrations."
}

// listProjects reads the organization's projects.
func listProjects(ctx context.Context, client platform.Client) ([]project, error) {
	var listing struct {
		Items []project `json:"items"`
	}
	err := client.Get(ctx, "/projects", &listing)
	return listing.Items, err
}

// integrationsOf reads one project's integrations, or every project's in turn
// when projectID is empty. The API lists one project at a time, at most 100
// integrations each, with no continuation cursor.
func integrationsOf(ctx context.Context, client platform.Client, projectID, endpoint string) ([]integration, error) {
	projectIDs := []string{projectID}
	if projectID == "" {
		projects, err := listProjects(ctx, client)
		if err != nil {
			return nil, callFailed(err, "list the projects", endpoint)
		}
		projectIDs = projectIDs[:0]
		for _, p := range projects {
			projectIDs = append(projectIDs, p.ID)
		}
	}
	var all []integration
	for _, id := range projectIDs {
		var listing struct {
			Items []integration `json:"items"`
		}
		if err := client.Get(ctx, projectIntegrationsPath(id), &listing); err != nil {
			return nil, callFailedNaming(err, "list the integrations of the project "+id, endpoint,
				projectNotFoundRecovery)
		}
		for _, c := range listing.Items {
			if c.Project == "" {
				c.Project = id
			}
			all = append(all, c)
		}
	}
	return all, nil
}

// projectFlag is the --project every integration command takes.
type projectFlag struct {
	project string
}

func (f *projectFlag) bind(command *cobra.Command, usage string) {
	command.Flags().StringVar(&f.project, "project", "", usage)
}

// integrationList answers "wso2 intg integration list [--project <project>]".
func integrationList(command *cobra.Command, flags *projectFlag) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		if err := noArguments(command.Flags().Args(), "Run wso2 intg integration list [--project <project>]."); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		found, err := integrationsOf(ctx, client, projectOf(flags.project, request), request.Context.Endpoint)
		if err != nil {
			return result.Result{}, err
		}
		report := result.New(IntegrationsSchema).
			With("count", "Integrations", strconv.Itoa(len(found))).
			WithColumn("name", "Name").
			WithColumn("id", "ID").
			WithColumn("project", "Project").
			WithColumn("type", "Type").
			WithColumn("runtime", "Runtime").
			WithColumn("status", "Status").
			WithColumn("updated", "Updated").
			WithColumn("state", "State")
		for _, c := range found {
			report = report.WithRow(c.Name, c.ID, c.Project, c.kind(), c.runtime(), orDash(c.Status),
				orDash(c.Updated), c.state())
		}
		next := "Run wso2 intg integration show <id> to see one integration."
		if len(found) == 0 {
			next = "There are no integrations yet. Create one with wso2 intg integration create <name> --project <project>."
		}
		return report.With(NextField, "Next", next), nil
	}
}

// findIntegration picks the integration ref names out of found: by ID first,
// since only IDs are unique, then by display name, refusing a display name
// that more than one integration carries.
func findIntegration(found []integration, ref, projectID string) (integration, error) {
	for _, c := range found {
		if c.ID == ref {
			return c, nil
		}
	}
	var named []integration
	for _, c := range found {
		if c.Name == ref {
			named = append(named, c)
		}
	}
	switch len(named) {
	case 1:
		return named[0], nil
	case 0:
		where := "in this organization"
		if projectID != "" {
			where = "in the project " + projectID
		}
		return integration{}, problem.New(problem.CategoryProductService, "intg.not_found",
			"no integration "+where+" has the ID or name "+ref).
			WithRecovery(integrationListRecovery(projectID))
	default:
		ids := make([]string, len(named))
		for i, c := range named {
			ids[i] = c.ID
		}
		return integration{}, usageProblem("more than one integration is named "+ref+": "+strings.Join(ids, ", "),
			"Name the integration by its ID instead.")
	}
}

// integrationShow answers "wso2 intg integration show <integration> [--project <project>]".
//
// The API has no read of one integration, so the integration is found in its
// project's listing, or in every project's when none is named, and its
// repository is read beside it.
func integrationShow(command *cobra.Command, flags *projectFlag) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		ref, err := exactlyOneArgument(command.Flags().Args(), "name the integration to show",
			"Run wso2 intg integration show <integration> [--project <project>].")
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		found, err := integrationsOf(ctx, client, projectOf(flags.project, request), request.Context.Endpoint)
		if err != nil {
			return result.Result{}, err
		}
		c, err := findIntegration(found, ref, projectOf(flags.project, request))
		if err != nil {
			return result.Result{}, err
		}
		var repository integrationRepository
		if err := client.Get(ctx, withProject(integrationPath(c.ID)+"/repository", c.Project), &repository); err != nil {
			return result.Result{}, callFailedNaming(err, "read the repository of the integration "+c.ID,
				request.Context.Endpoint, integrationListRecovery(c.Project))
		}
		next := "Run wso2 intg integration list --project " + c.Project + " to see the other integrations in its project."
		if c.Deleting {
			next = "The integration is being deleted. Run wso2 intg integration list --project " + c.Project +
				" until it no longer appears."
		}
		return result.New(IntegrationSchema).
			With("id", "ID", c.ID).
			With("name", "Name", c.Name).
			With("project", "Project", c.Project).
			With("type", "Type", c.kind()).
			With("runtime", "Runtime", c.runtime()).
			With("description", "Description", orDash(c.Description)).
			With("status", "Status", orDash(c.Status)).
			With("repository", "Repository", repository.describe()).
			With("branch", "Branch", orDash(repository.Branch)).
			With("path", "Path", orDash(strings.Trim(repository.Path, "/"))).
			With("updated", "Updated", orDash(c.Updated)).
			With("created", "Created", orDash(c.Created)).
			With("state", "State", c.state()).
			With(NextField, "Next", next), nil
	}
}

// integrationCreateFlags are the options of "wso2 intg integration create".
type integrationCreateFlags struct {
	project     string
	kind        string
	runtime     string
	repo        string
	branch      string
	path        string
	displayName string
	description string
	autoDeploy  bool
}

func (f *integrationCreateFlags) bind(command *cobra.Command) {
	flags := command.Flags()
	flags.StringVar(&f.project, "project", "", "The project to create it in. Defaults to the context's project; "+
		"required when the context names none.")
	flags.StringVar(&f.kind, "type", "", "What it is: "+strings.Join(slices.Sorted(maps.Keys(integrationTypes)), ", ")+".")
	flags.StringVar(&f.runtime, "runtime", "", "What its source is built with: "+
		strings.Join(slices.Sorted(maps.Keys(integrationRuntimes)), ", ")+".")
	flags.StringVar(&f.repo, "repo", "", "The https clone URL of a public git repository.")
	flags.StringVar(&f.branch, "branch", "", "The branch to build.")
	flags.StringVar(&f.path, "path", "", "The directory of the integration within the repository. "+
		"Defaults to the repository root.")
	flags.StringVar(&f.displayName, "display-name", "", "The name people see. Defaults to the integration's name.")
	flags.StringVar(&f.description, "description", "", "What the integration does.")
	flags.BoolVar(&f.autoDeploy, "auto-deploy", false, "Deploy to the first environment after every successful build.")
}

// createRequest builds the platform-shaped body the API forwards upstream,
// exactly as the platform's own create_integration tool does. autoBuild is
// always set: it starts the first build, without which a new integration has
// nothing to deploy.
func (f *integrationCreateFlags) createRequest(name, project string) (map[string]any, error) {
	recovery := "Run wso2 intg integration create <name> --project <project> --type <type> --runtime <runtime> " +
		"--repo <url> --branch <branch>."
	if !integrationName.MatchString(name) {
		return nil, usageProblem("the integration name "+name+" is not one the platform accepts: use lowercase "+
			"letters, digits and hyphens, at most 63 characters", recovery)
	}
	// In a fixed order, so the same command line always names the same flag.
	for _, required := range []struct{ flag, value string }{
		{"--project", project}, {"--type", f.kind}, {"--runtime", f.runtime}, {"--repo", f.repo},
		{"--branch", f.branch},
	} {
		if required.value == "" && required.flag == "--project" {
			return nil, usageProblem("--project is required, since the selected context names no project",
				recovery+" Or record a project on the context, which every command then defaults to.")
		}
		if required.value == "" {
			return nil, usageProblem(required.flag+" is required", recovery)
		}
	}
	integrationType, known := integrationTypes[f.kind]
	if !known {
		return nil, usageProblem("--type "+f.kind+" is not an integration type", "Use one of "+
			strings.Join(slices.Sorted(maps.Keys(integrationTypes)), ", ")+".")
	}
	workflow, known := integrationRuntimes[f.runtime]
	if !known {
		return nil, usageProblem("--runtime "+f.runtime+" is not a runtime", "Use one of "+
			strings.Join(slices.Sorted(maps.Keys(integrationRuntimes)), ", ")+".")
	}
	if !strings.HasPrefix(f.repo, "https://") {
		return nil, usageProblem("--repo "+f.repo+" is not an https clone URL",
			"Give the repository's https clone URL, such as https://github.com/<organization>/<repository>.git.")
	}
	annotations := map[string]string{}
	if f.displayName != "" {
		annotations["openchoreo.dev/display-name"] = f.displayName
	}
	if f.description != "" {
		annotations["openchoreo.dev/description"] = f.description
	}
	repository := map[string]any{"url": f.repo, "revision": map[string]string{"branch": f.branch}}
	if f.path != "" {
		repository["appPath"] = f.path
	}
	return map[string]any{
		"metadata": map[string]any{"name": name, "annotations": annotations},
		"spec": map[string]any{
			"owner":         map[string]string{"projectName": project},
			"componentType": map[string]string{"name": integrationType},
			"autoBuild":     true,
			"autoDeploy":    f.autoDeploy,
			"workflow": map[string]any{
				"kind":       "ClusterWorkflow",
				"name":       workflow,
				"parameters": map[string]any{"repository": repository},
			},
		},
	}, nil
}

// integrationCreate answers "wso2 intg integration create <name> ...".
func integrationCreate(command *cobra.Command, flags *integrationCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := exactlyOneArgument(command.Flags().Args(), "name the integration to create",
			"Run wso2 intg integration create <name> --project <project> --type <type> --runtime <runtime> "+
				"--repo <url> --branch <branch>.")
		if err != nil {
			return result.Result{}, err
		}
		project := projectOf(flags.project, request)
		body, err := flags.createRequest(name, project)
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var created integration
		if err := client.Post(ctx, projectIntegrationsPath(project), body, &created); err != nil {
			return result.Result{}, integrationCreateFailed(err, name, request.Context.Endpoint)
		}
		if created.ID == "" {
			created.ID = name
		}
		if created.Project == "" {
			created.Project = project
		}
		// Integration names are unique across the organization, and the
		// platform suffixes a taken one rather than refusing it.
		next := "Its first build has started. Run wso2 intg integration show " + created.ID + " --project " +
			created.Project + " to follow it."
		if created.ID != name {
			next = "The platform created it as " + created.ID + " because " + name + " was taken. " + next
		}
		return result.New(CreatedIntegrationSchema).
			With("id", "ID", created.ID).
			With("name", "Name", orDash(created.Name)).
			With("project", "Project", created.Project).
			With("type", "Type", created.kind()).
			With("runtime", "Runtime", created.runtime()).
			With("warning", "Warning", orDash(created.Warning)).
			With(NextField, "Next", next), nil
	}
}

// integrationCreateFailed maps the refusals that mean something particular for a
// create, and hands every other one to callFailed.
func integrationCreateFailed(err error, name, endpoint string) error {
	if refused, ok := refusal(err, http.StatusConflict); ok {
		return problem.New(problem.CategoryProductService, "intg.integration_exists",
			"the platform could not find a free name for the integration "+name+": "+refusalWords(refused)).
			WithRecovery("Choose another name and run wso2 intg integration create again.")
	}
	if refused, ok := refusal(err, http.StatusBadRequest); ok {
		return problem.New(problem.CategoryUsage, "intg.call_failed",
			"the Integration Platform refused to create the integration "+name+": "+refusalWords(refused)).
			WithRecovery("Check the values given on the command line, then run wso2 intg integration create again.")
	}
	return callFailed(err, "create the integration "+name, endpoint)
}

// integrationDeleteFlags are the options of "wso2 intg integration delete".
type integrationDeleteFlags struct {
	projectFlag
	removalFlags
}

// integrationDelete answers "wso2 intg integration delete <integration>",
// asking first unless --yes is given.
//
// The project is optional: the API resolves it from the integration, whose
// name is unique across the organization.
func integrationDelete(command *cobra.Command, flags *integrationDeleteFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		id, err := exactlyOneArgument(command.Flags().Args(), "name the integration to delete",
			"Run wso2 intg integration delete <integration>.")
		if err != nil {
			return result.Result{}, err
		}
		if err := confirmRemoval(ctx, request, &flags.removalFlags, "Delete the integration "+id+
			", its builds and its deployments? This cannot be undone."); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		project := projectOf(flags.project, request)
		if err := client.Delete(ctx, withProject(integrationPath(id), project)); err != nil {
			return result.Result{}, callFailedNaming(err, "delete the integration "+id, request.Context.Endpoint,
				integrationListRecovery(project))
		}
		return result.New(IntegrationRemovalSchema).
			With("integration", "Integration", id).
			With("state", "State", "deleting").
			With(NextField, "Next", "Deletion continues in the background. Run wso2 intg integration list "+
				"until the integration no longer appears."), nil
	}
}
