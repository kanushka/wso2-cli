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
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// ProjectsSchema identifies the project listing.
const ProjectsSchema = "intg.projects/v1"

// ProjectSchema identifies one project, as project show and project create
// report it.
const ProjectSchema = "intg.project/v1"

// ProjectRemovalSchema identifies an accepted project delete.
const ProjectRemovalSchema = "intg.projectRemoval/v1"

// project is the part of the API's Project this module reports. The API
// addresses projects by resource name, so id is what every other command
// takes, and name is the display name.
type project struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	Pipeline        string `json:"defaultDeploymentPipelineId"`
	GitProvider     string `json:"gitProvider"`
	GitOrganization string `json:"gitOrganization"`
	Repository      string `json:"repository"`
	Branch          string `json:"branch"`
	DirectoryPath   string `json:"directoryPath"`
	Created         string `json:"createdDate"`
	Deleting        bool   `json:"deleting"`
}

// repository names the linked repository as organization/name, or "-" when
// the project links none.
func (p project) repository() string {
	switch {
	case p.GitOrganization != "" && p.Repository != "":
		return p.GitOrganization + "/" + p.Repository
	case p.Repository != "":
		return p.Repository
	default:
		return "-"
	}
}

// qualifiedRepository is repository with the git provider in front, which a
// single project has room for and a listing does not.
func (p project) qualifiedRepository() string {
	if p.GitProvider == "" || p.repository() == "-" {
		return p.repository()
	}
	return p.GitProvider + ": " + p.repository()
}

// state is "deleting" while an accepted delete is still running, and "active"
// otherwise.
func (p project) state() string {
	if p.Deleting {
		return "deleting"
	}
	return "active"
}

// orDash stands in for an empty value so a column or field never reads as
// missing.
func orDash(value string) string {
	if value == "" {
		return "-"
	}
	return value
}

// projectPath is the API path of one project, its name escaped as a single
// segment.
func projectPath(name string) string {
	return "/projects/" + url.PathEscape(name)
}

// projectNotFoundRecovery is where to find a project's ID.
const projectNotFoundRecovery = "Run wso2 intg project list to see the IDs of this organization's projects."

// projectReport is one project in display order.
func projectReport(p project, next string) result.Result {
	return result.New(ProjectSchema).
		With("id", "ID", p.ID).
		With("name", "Name", p.Name).
		With("description", "Description", orDash(p.Description)).
		With("pipeline", "Pipeline", orDash(p.Pipeline)).
		With("repository", "Repository", p.qualifiedRepository()).
		With("branch", "Branch", orDash(p.Branch)).
		With("path", "Path", orDash(p.DirectoryPath)).
		With("created", "Created", orDash(p.Created)).
		With("state", "State", p.state()).
		With(NextField, "Next", next)
}

// projectList answers "wso2 intg project list".
//
// The API returns at most one page of projects for the organization the
// token names and carries no continuation cursor, so there is nothing further
// to read.
func projectList(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		if err := noArguments(command.Flags().Args(), "Run wso2 intg project list."); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var listing struct {
			Items []project `json:"items"`
		}
		if err := client.Get(ctx, "/projects", &listing); err != nil {
			return result.Result{}, callFailed(err, "list the projects", request.Context.Endpoint)
		}

		report := result.New(ProjectsSchema).
			With("count", "Projects", strconv.Itoa(len(listing.Items))).
			WithColumn("name", "Name").
			WithColumn("id", "ID").
			WithColumn("pipeline", "Pipeline").
			WithColumn("repository", "Repository").
			WithColumn("state", "State")
		for _, p := range listing.Items {
			report = report.WithRow(p.Name, p.ID, orDash(p.Pipeline), p.repository(), p.state())
		}
		next := "Run wso2 intg project show <id> to see one project."
		if len(listing.Items) == 0 {
			next = "This organization has no projects yet. Create one with wso2 intg project create <name>."
		}
		return report.With(NextField, "Next", next), nil
	}
}

// projectShow answers "wso2 intg project show <project>".
func projectShow(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := exactlyOneArgument(command.Flags().Args(), "name the project to show",
			"Run wso2 intg project show <project>, with an ID from wso2 intg project list.")
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var found project
		if err := client.Get(ctx, projectPath(name), &found); err != nil {
			return result.Result{}, callFailedNaming(err, "show the project "+name, request.Context.Endpoint,
				projectNotFoundRecovery)
		}
		next := "Use --project " + found.ID + " with the commands that work inside a project."
		if found.Deleting {
			next = "The project is being deleted. Run wso2 intg project list until it no longer appears."
		}
		return projectReport(found, next), nil
	}
}

// projectCreateFlags are the options of "wso2 intg project create".
type projectCreateFlags struct {
	displayName string
	description string
	pipeline    string
	repo        string
	branch      string
	path        string
}

// bind declares the flags on command.
func (f *projectCreateFlags) bind(command *cobra.Command) {
	flags := command.Flags()
	flags.StringVar(&f.displayName, "display-name", "", "The name people see. Defaults to the project's name.")
	flags.StringVar(&f.description, "description", "", "What the project is for.")
	flags.StringVar(&f.pipeline, "pipeline", "", "The deployment pipeline to bind it to. Defaults to the "+
		"organization's default pipeline.")
	flags.StringVar(&f.repo, "repo", "", "A GitHub or Bitbucket repository URL to link the project to.")
	flags.StringVar(&f.branch, "branch", "", "The branch of --repo to build from. Required with --repo.")
	flags.StringVar(&f.path, "path", "", "The directory within --repo the project's integrations live in.")
}

// createRequest is the API's CreateProjectRequest with only the given members.
// The repository link is sent whole or not at all: the API drops a partial
// one without saying so.
func (f *projectCreateFlags) createRequest(name string) (map[string]any, error) {
	body := map[string]any{"name": name}
	for member, value := range map[string]string{
		"displayName": f.displayName, "description": f.description, "deploymentPipeline": f.pipeline,
	} {
		if value != "" {
			body[member] = value
		}
	}
	if f.repo == "" {
		if f.branch != "" || f.path != "" {
			return nil, usageProblem("--branch and --path describe a repository link, and no --repo was given",
				"Run wso2 intg project create "+name+" --repo <url> --branch <branch> [--path <dir>].")
		}
		return body, nil
	}
	if f.branch == "" {
		return nil, usageProblem("a repository link needs the branch to build from",
			"Run wso2 intg project create "+name+" --repo "+f.repo+" --branch <branch>.")
	}
	provider, organization, repository, err := parseRepository(f.repo)
	if err != nil {
		return nil, err
	}
	body["gitProvider"] = provider
	body["gitOrganization"] = organization
	body["repository"] = repository
	body["branch"] = f.branch
	if path := strings.Trim(f.path, "/"); path != "" {
		body["directoryPath"] = path
	}
	return body, nil
}

// repositoryHosts maps the hosts a repository URL may name to the API's git
// provider. Others, such as a self-hosted GitLab, are linked in the console.
var repositoryHosts = map[string]string{"github.com": "github", "bitbucket.org": "bitbucket"}

// parseRepository reads https://<host>/<organization>/<repository>[.git],
// with or without the scheme.
func parseRepository(raw string) (provider, organization, repository string, err error) {
	trimmed := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), ".git")
	trimmed = strings.TrimPrefix(strings.TrimPrefix(trimmed, "https://"), "http://")
	parts := strings.Split(trimmed, "/")
	recovery := "Give --repo as https://github.com/<organization>/<repository> or " +
		"https://bitbucket.org/<workspace>/<repository>."
	if len(parts) != 3 || parts[1] == "" || parts[2] == "" {
		return "", "", "", usageProblem("--repo "+raw+" does not name an organization and a repository", recovery)
	}
	provider, known := repositoryHosts[strings.ToLower(parts[0])]
	if !known {
		return "", "", "", usageProblem("--repo names "+parts[0]+", and only GitHub and Bitbucket repositories "+
			"can be linked from the CLI", recovery+" Link other repositories in the Integration Platform console.")
	}
	return provider, parts[1], parts[2], nil
}

// projectCreate answers "wso2 intg project create <name>".
func projectCreate(command *cobra.Command, flags *projectCreateFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := exactlyOneArgument(command.Flags().Args(), "name the project to create",
			"Run wso2 intg project create <name>.")
		if err != nil {
			return result.Result{}, err
		}
		body, err := flags.createRequest(name)
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var created project
		if err := client.Post(ctx, "/projects", body, &created); err != nil {
			return result.Result{}, projectCreateFailed(err, name, request.Context.Endpoint)
		}
		if created.ID == "" {
			created.ID = name
		}
		return projectReport(created, "Run wso2 intg project show "+created.ID+" to see it."), nil
	}
}

// projectCreateFailed maps the refusals that mean something particular for a
// create, and hands every other one to callFailed.
func projectCreateFailed(err error, name, endpoint string) error {
	if refused, ok := refusal(err, http.StatusConflict); ok {
		return problem.New(problem.CategoryProductService, "intg.project_exists",
			"a different project named "+name+" already exists: "+refusalWords(refused)).
			WithRecovery("Run wso2 intg project show " + name + " to see it, or choose another name.")
	}
	if refused, ok := refusal(err, http.StatusServiceUnavailable); ok {
		// The API's 503 here means the project exists but its per-environment
		// release bindings are not all in place, and that the same request
		// completes them.
		return problem.New(problem.CategoryProductService, "intg.project_incomplete",
			"the project "+name+" was created but is not fully set up: "+refusalWords(refused)).
			WithRecovery("Run wso2 intg project create " + name + " again with the same options to finish it.")
	}
	if refused, ok := refusal(err, http.StatusBadRequest); ok {
		return problem.New(problem.CategoryUsage, "intg.call_failed",
			"the Integration Platform refused to create the project "+name+": "+refusalWords(refused)).
			WithRecovery("Check the values given on the command line, then run wso2 intg project create " +
				name + " again.")
	}
	return callFailed(err, "create the project "+name, endpoint)
}

// removalFlags carry the --yes every delete takes.
type removalFlags struct {
	yes bool
}

func (f *removalFlags) bind(command *cobra.Command) {
	command.Flags().BoolVar(&f.yes, "yes", false, "Delete without asking first.")
}

// confirmRemoval asks the person running the command before something is
// deleted, unless --yes answered in advance. The shell draws the question,
// and refuses it under --no-input with a recovery naming --yes; that refusal
// is returned unchanged.
func confirmRemoval(ctx context.Context, request module.Request, flags *removalFlags, question string) error {
	if flags.yes {
		return nil
	}
	return request.Prompt.Confirm(ctx, module.ConfirmRequest{Question: question})
}

// projectDelete answers "wso2 intg project delete <project>", asking first
// unless --yes is given. Nothing is called until the deletion is confirmed.
func projectDelete(command *cobra.Command, flags *removalFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		name, err := exactlyOneArgument(command.Flags().Args(), "name the project to delete",
			"Run wso2 intg project delete <project>.")
		if err != nil {
			return result.Result{}, err
		}
		if err := confirmRemoval(ctx, request, flags, "Delete the project "+name+
			" and every integration in it? This cannot be undone."); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		if err := client.Delete(ctx, projectPath(name)); err != nil {
			return result.Result{}, callFailedNaming(err, "delete the project "+name, request.Context.Endpoint,
				projectNotFoundRecovery)
		}
		return result.New(ProjectRemovalSchema).
			With("project", "Project", name).
			With("state", "State", "deleting").
			With(NextField, "Next", "Deletion continues in the background. Run wso2 intg project list "+
				"until the project no longer appears."), nil
	}
}
