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
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/internal/contexts"
	"github.com/wso2/wso2-cli/internal/output"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

const (
	projectsSchema = "shell.projects/v1"
	projectSchema  = "shell.project/v1"

	scopeProjectRead   = "cloud:project:read"
	scopeProjectCreate = "cloud:project:create"
	scopeProjectDelete = "cloud:project:delete"

	projectPageSize = 100
	// projectPageLimit stops a service whose cursor never ends from paging
	// forever.
	projectPageLimit = 100

	projectListNext = "Run wso2 project list to see the projects, then "

	projectUseUsage    = "Run wso2 project use <project>."
	projectCreateUsage = "Run wso2 project create <display-name> [--id <id>] [--description <text>] " +
		"[--pipeline <pipeline>]."
	projectDeleteUsage = "Run wso2 project delete <project> [--yes]."

	displayNameAnnotation = "openchoreo.dev/display-name"
	descriptionAnnotation = "openchoreo.dev/description"
	defaultPipeline       = "default"
)

// projectIDPattern is a Kubernetes DNS-1123 label, which the platform
// service requires of a project's id.
var projectIDPattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// reservedProjectIDs name pages that stand where a project's would in the
// WSO2 Cloud consoles, which the console refuses as ids too.
var reservedProjectIDs = []string{"new", "create", "redirect"}

// cloudProject is one project as the shell reports it.
type cloudProject struct {
	ID          string
	Name        string
	Description string
	Pipeline    string
	Deleting    bool
}

// openChoreoProject is the platform service's project body.
type openChoreoProject struct {
	Metadata struct {
		Name              string            `json:"name"`
		Annotations       map[string]string `json:"annotations,omitempty"`
		DeletionTimestamp string            `json:"deletionTimestamp,omitempty"`
	} `json:"metadata"`
	Spec struct {
		DeploymentPipelineRef *struct {
			Kind string `json:"kind,omitempty"`
			Name string `json:"name"`
		} `json:"deploymentPipelineRef,omitempty"`
		Type *struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"type,omitempty"`
	} `json:"spec"`
}

func (p openChoreoProject) project() cloudProject {
	project := cloudProject{
		ID:          p.Metadata.Name,
		Name:        strings.TrimSpace(p.Metadata.Annotations[displayNameAnnotation]),
		Description: strings.TrimSpace(p.Metadata.Annotations[descriptionAnnotation]),
		Pipeline:    defaultPipeline,
		Deleting:    p.Metadata.DeletionTimestamp != "",
	}
	if project.Name == "" {
		project.Name = project.ID
	}
	if p.Spec.DeploymentPipelineRef != nil && p.Spec.DeploymentPipelineRef.Name != "" {
		project.Pipeline = p.Spec.DeploymentPipelineRef.Name
	}
	return project
}

func (s Shell) projectCommand() *cobra.Command {
	command := &cobra.Command{
		Use:                   "project <subcommand>",
		Short:                 "Select, create, and delete WSO2 Cloud projects.",
		Long:                  projectRecovery,
		DisableFlagsInUseLine: true,
		RunE: func(command *cobra.Command, args []string) error {
			if len(args) == 0 {
				return helpForBareFamily(command)
			}
			return problem.New(problem.CategoryUsage, "shell.unknown_command",
				fmt.Sprintf("%q is not a wso2 project subcommand", args[0])).
				WithRecovery(projectRecovery)
		},
	}
	command.AddCommand(s.projectListCommand(), s.projectUseCommand(), s.projectCurrentCommand(),
		s.projectCreateCommand(), s.projectDeleteCommand())
	return command
}

const projectRecovery = "Run wso2 project list to see the organization's projects, wso2 project use <project> " +
	"to select one, or wso2 project current to show the selected one."

func (s Shell) projectListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List the organization's projects, marking the selected one.",
		Args:  noArguments("Run wso2 project list."),
		RunE: func(command *cobra.Command, _ []string) error {
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			client, selected, err := s.platform("list projects", scopeProjectRead)
			if err != nil {
				return err
			}
			projects, err := client.projects()
			if err != nil {
				return err
			}
			return output.Result(s.Streams.Out, mode, projectListing(projects, selected.Context.Project))
		},
	}
}

func projectListing(projects []cloudProject, selected string) result.Result {
	listing := result.New(projectsSchema).With("count", "Projects", strconv.Itoa(len(projects))).
		WithColumn("selected", "").WithColumn("name", "Name").WithColumn("id", "ID").
		WithColumn("pipeline", "Pipeline").WithColumn("description", "Description")
	for _, project := range projects {
		mark := ""
		if project.ID == selected {
			mark = "*"
		}
		description := project.Description
		if project.Deleting {
			description = strings.TrimSpace("(deleting) " + description)
		}
		listing = listing.WithRow(mark, project.Name, project.ID, project.Pipeline, description)
	}
	if len(projects) > 0 {
		listing = listing.With("next", "", fmt.Sprintf("Run wso2 project use %s to select a project.",
			projects[0].ID))
	} else {
		listing = listing.With("next", "", "Run wso2 project create <display-name> to create one.")
	}
	return listing
}

// projects is every project in the organization, in the order the service
// lists them.
func (c platformClient) projects() ([]cloudProject, error) {
	var all []cloudProject
	cursor := ""
	for range projectPageLimit {
		query := url.Values{"limit": {strconv.Itoa(projectPageSize)}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}
		var page struct {
			Items      []openChoreoProject `json:"items"`
			Pagination struct {
				NextCursor string `json:"nextCursor"`
			} `json:"pagination"`
		}
		if err := c.call(http.MethodGet, "/admin/projects?"+query.Encode(), nil, &page, "list projects"); err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			all = append(all, item.project())
		}
		if page.Pagination.NextCursor == "" {
			return all, nil
		}
		if page.Pagination.NextCursor == cursor {
			break
		}
		cursor = page.Pagination.NextCursor
	}
	return nil, problem.New(problem.CategoryProductService, "shell.platform_answer_malformed",
		"WSO2 Cloud's platform service kept answering the project list with another page").
		WithRecovery("Retry the command. Report it if it keeps happening.")
}

// find is the one project an id or a display name names: an id first, then a
// display name, ignoring case.
func findProject(projects []cloudProject, named string) (cloudProject, error) {
	for _, project := range projects {
		if project.ID == named {
			return project, nil
		}
	}
	var matched []cloudProject
	for _, project := range projects {
		if strings.EqualFold(project.Name, named) {
			matched = append(matched, project)
		}
	}
	switch len(matched) {
	case 1:
		return matched[0], nil
	case 0:
		return cloudProject{}, problem.New(problem.CategoryNotFound, "shell.project_not_found",
			fmt.Sprintf("the organization has no project whose id or name is %q", named)).
			WithRecovery(projectListNext + "name one by its id.")
	}
	ids := make([]string, 0, len(matched))
	for _, project := range matched {
		ids = append(ids, project.ID)
	}
	return cloudProject{}, problem.New(problem.CategoryUsage, "shell.project_ambiguous",
		fmt.Sprintf("%d projects are named %q: %s", len(matched), named, strings.Join(ids, ", "))).
		WithRecovery("Name the project by its id.")
}

func (s Shell) projectUseCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "use <project>",
		Short: "Select a project, by id or display name, for the selected context.",
		Args:  exactlyOneArgument("a project id or name", projectListNext+"wso2 project use <project>."),
		RunE: func(command *cobra.Command, args []string) error {
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			client, selected, err := s.platform("list projects", scopeProjectRead)
			if err != nil {
				return err
			}
			projects, err := client.projects()
			if err != nil {
				return err
			}
			project, err := findProject(projects, args[0])
			if err != nil {
				return err
			}
			if project.Deleting {
				return problem.New(problem.CategoryConflict, "shell.project_deleting",
					fmt.Sprintf("the %q project is being deleted", project.ID)).
					WithRecovery(projectListNext + "select another.")
			}
			if err := s.recordProject(selected.Context.Name, project.ID); err != nil {
				return err
			}
			return output.Result(s.Streams.Out, mode, result.New(projectSchema).
				With("context", "Context", selected.Context.Name).
				With("project", "Project", project.ID).
				With("name", "Name", project.Name))
		},
	}
}

// recordProject writes the project the named context selects, refusing when
// the selected context changed meanwhile.
func (s Shell) recordProject(name, project string) error {
	root, err := s.stateRoot()
	if err != nil {
		return err
	}
	err = contexts.Update(root, func(document contexts.Document) (contexts.Document, error) {
		if still, err := document.Select(""); err != nil || still.Context.Name != name {
			return document, problem.New(problem.CategoryUsage, "contexts.document_busy",
				"the selected context changed while the project was being set").
				WithRecovery("Run the same wso2 project command again.")
		}
		for i := range document.Contexts {
			if document.Contexts[i].Name == name {
				document.Contexts[i].Project = project
			}
		}
		return document, nil
	})
	if err != nil {
		return s.explainWriteRefusal(root, err)
	}
	return nil
}

func (s Shell) projectCurrentCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "current",
		Short: "Show the project the selected context selects.",
		Args:  noArguments("Run wso2 project current."),
		RunE: func(command *cobra.Command, _ []string) error {
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			selected, err := s.selection("")
			if err != nil {
				return err
			}
			if err := requirePlatform(selected); err != nil {
				return err
			}
			current := result.New(projectSchema).
				With("context", "Context", selected.Context.Name).
				With("project", "Project", selected.Context.Project)
			if selected.Context.Project != "" {
				return output.Result(s.Streams.Out, mode, current)
			}
			next := projectListNext + "wso2 project use <project> to select one."
			if mode == output.ModeJSON {
				return output.Result(s.Streams.Out, mode, current.With("next", "", next))
			}
			_, err = fmt.Fprintf(s.Streams.Out, output.Hint(s.Streams.Out,
				"The %q context selects no project.\n\n%s\n"), selected.Context.Name, next)
			return err
		},
	}
}

type projectCreateFlags struct {
	id, description, pipeline string
}

func (s Shell) projectCreateCommand() *cobra.Command {
	var flags projectCreateFlags
	command := &cobra.Command{
		Use:   "create <display-name>",
		Short: "Create a project in the organization.",
		Long: "Create a project in the organization.\n\n" +
			"Its id is derived from the display name, as the WSO2 Cloud console derives it, unless --id " +
			"names one. Creating a project does not select it.",
		Args: exactlyOneArgument("the project's display name", projectCreateUsage),
		RunE: func(command *cobra.Command, args []string) error {
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			body, err := newProjectBody(args[0], flags)
			if err != nil {
				return err
			}
			client, _, err := s.platform("create projects", scopeProjectCreate)
			if err != nil {
				return err
			}
			var created openChoreoProject
			if err := client.call(http.MethodPost, "/admin/projects", body, &created,
				"create the "+strconv.Quote(body.Metadata.Name)+" project"); err != nil {
				return err
			}
			project := body.project()
			if created.Metadata.Name != "" {
				project = created.project()
			}
			return output.Result(s.Streams.Out, mode, result.New(projectSchema).
				With("project", "ID", project.ID).
				With("name", "Name", project.Name).
				With("pipeline", "Pipeline", project.Pipeline).
				With("next", "", fmt.Sprintf("Run wso2 project use %s to select it.", project.ID)))
		},
	}
	f := command.Flags()
	f.StringVar(&flags.id, "id", "", "The project's id, instead of the one derived from its display name.")
	f.StringVar(&flags.description, "description", "", "What the project is for.")
	f.StringVar(&flags.pipeline, "pipeline", defaultPipeline, "The deployment pipeline the project promotes through.")
	return command
}

// newProjectBody is the body that creates a project, after the limits the
// console holds a project to.
func newProjectBody(displayName string, flags projectCreateFlags) (openChoreoProject, error) {
	var body openChoreoProject
	displayName = strings.TrimSpace(displayName)
	invalid := func(message, recovery string) error {
		return problem.New(problem.CategoryUsage, "shell.invalid_argument", message).WithRecovery(recovery)
	}
	if displayName == "" || len([]rune(displayName)) > 100 {
		return body, invalid("a project's display name needs 1 to 100 characters", projectCreateUsage)
	}
	if len([]rune(strings.TrimSpace(flags.description))) > 500 {
		return body, invalid("a project's description takes at most 500 characters", projectCreateUsage)
	}
	id := flags.id
	if id == "" {
		id = projectID(displayName)
	}
	switch {
	case len(id) < 3 || len(id) > 63 || !projectIDPattern.MatchString(id):
		return body, invalid(fmt.Sprintf("%q is not a project id: an id has 3 to 63 lowercase letters, digits "+
			"and hyphens, starting and ending with a letter or digit", id),
			"Name one with --id <id>.")
	case slices.Contains(reservedProjectIDs, id):
		return body, invalid(fmt.Sprintf("%q is reserved and cannot be a project id", id),
			"Name another with --id <id>.")
	}
	pipeline := strings.TrimSpace(flags.pipeline)
	if pipeline == "" {
		pipeline = defaultPipeline
	}
	body.Metadata.Name = id
	body.Metadata.Annotations = map[string]string{displayNameAnnotation: displayName}
	if description := strings.TrimSpace(flags.description); description != "" {
		body.Metadata.Annotations[descriptionAnnotation] = description
	}
	body.Spec.DeploymentPipelineRef = &struct {
		Kind string `json:"kind,omitempty"`
		Name string `json:"name"`
	}{Kind: "DeploymentPipeline", Name: pipeline}
	// Without it, OpenChoreo falls back to a cluster-wide project type; each
	// organization has its own.
	body.Spec.Type = &struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}{Kind: "ProjectType", Name: "default"}
	return body, nil
}

// projectID derives an id from a display name the way the WSO2 Cloud
// console does: lowercased, every run of other characters a hyphen, at most
// 63 characters. The console also folds accented letters to their base
// letter; here they become hyphens, and --id names an id where that matters.
func projectID(displayName string) string {
	var id strings.Builder
	hyphen := false
	for _, r := range strings.ToLower(displayName) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			id.WriteRune(r)
			hyphen = false
			continue
		}
		if !hyphen {
			id.WriteByte('-')
			hyphen = true
		}
	}
	derived := strings.Trim(id.String(), "-")
	if len(derived) > 63 {
		derived = strings.TrimRight(derived[:63], "-")
	}
	return derived
}

func (s Shell) projectDeleteCommand() *cobra.Command {
	var yes bool
	command := &cobra.Command{
		Use:   "delete <project>",
		Short: "Delete a project, by id or display name, from the organization.",
		Long: "Delete a project, by id or display name, from the organization.\n\n" +
			"WSO2 Cloud refuses to delete a project that still holds APIs, integrations or other items. " +
			"An accepted delete finishes in the background. Deleting the project the selected context " +
			"selects clears that selection.",
		Args: exactlyOneArgument("a project id or name", projectListNext+"wso2 project delete <project>."),
		RunE: func(command *cobra.Command, args []string) error {
			mode, err := s.shellOutputMode(command)
			if err != nil {
				return err
			}
			// The token is acquired before asking, so a role that may not
			// delete is told so before anyone confirms.
			client, selected, err := s.platform("delete projects", scopeProjectRead, scopeProjectDelete)
			if err != nil {
				return err
			}
			projects, err := client.projects()
			if err != nil {
				return err
			}
			project, err := findProject(projects, args[0])
			if err != nil {
				return err
			}
			if err := s.confirmProjectDelete(yes, project); err != nil {
				return err
			}
			if err := client.call(http.MethodDelete, "/admin/projects/"+url.PathEscape(project.ID), nil, nil,
				"delete the "+strconv.Quote(project.ID)+" project"); err != nil {
				return err
			}
			if selected.Context.Project == project.ID {
				if err := s.recordProject(selected.Context.Name, ""); err != nil {
					return err
				}
				if _, err := fmt.Fprintf(s.Streams.Err, "The %q context no longer selects a project.\n",
					selected.Context.Name); err != nil {
					return err
				}
			}
			return output.Result(s.Streams.Out, mode, result.New(projectSchema).
				With("project", "ID", project.ID).
				With("name", "Name", project.Name).
				With("state", "State", "deleting"))
		},
	}
	command.Flags().BoolVar(&yes, "yes", false, "Delete without asking.")
	return command
}

func (s Shell) confirmProjectDelete(yes bool, project cloudProject) error {
	if yes {
		return nil
	}
	subject := fmt.Sprintf("deleting the %q project", project.ID)
	if may, reason := s.mayPrompt(false); !may {
		return nonInteractiveConfirmation(subject, reason)
	}
	confirmed, err := s.confirm(fmt.Sprintf("Delete the %q project (%s)? This cannot be undone.",
		project.Name, project.ID))
	if err != nil {
		return err
	}
	if !confirmed {
		return declinedConfirmation()
	}
	return nil
}
