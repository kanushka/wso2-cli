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
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/wso2/wso2-cli/modules/intg/internal/platform"
	"github.com/wso2/wso2-cli/sdk/module"
	"github.com/wso2/wso2-cli/sdk/problem"
	"github.com/wso2/wso2-cli/sdk/result"
)

// BuildStartSchema identifies an accepted build request.
const BuildStartSchema = "intg.buildStart/v1"

// BuildsSchema identifies an integration's build listing.
const BuildsSchema = "intg.builds/v1"

// BuildSchema identifies one build.
const BuildSchema = "intg.build/v1"

// BuildLogsSchema identifies one build's log.
const BuildLogsSchema = "intg.buildLogs/v1"

// fullCommitSHA is the only commit form a build can be pinned to: the build
// fetches the commit as a ref, which git allows only for a full SHA.
var fullCommitSHA = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

// shortCommitLength is how much of a commit a listing shows.
const shortCommitLength = 12

// build is the part of the API's WorkflowRun this module reports. Builds are
// named by the platform, and a name is what build show and build logs take.
type build struct {
	Name        string `json:"name"`
	Status      string `json:"status"`
	Trigger     string `json:"trigger"`
	Integration string `json:"componentName"`
	Project     string `json:"projectName"`
	Commit      string `json:"commit"`
	Image       string `json:"image"`
	Started     string `json:"startedAt"`
	Completed   string `json:"completedAt"`
	Tasks       []struct {
		Name  string `json:"name"`
		Phase string `json:"phase"`
	} `json:"tasks"`
}

func (b build) shortCommit() string {
	if len(b.Commit) > shortCommitLength {
		return b.Commit[:shortCommitLength]
	}
	return orDash(b.Commit)
}

// steps is one "name: phase" line per step, or "-" when the build states none.
func (b build) steps() string {
	lines := make([]string, len(b.Tasks))
	for i, task := range b.Tasks {
		lines[i] = task.Name + ": " + orDash(task.Phase)
	}
	return orDash(strings.Join(lines, "\n"))
}

func buildsPath(integrationID string) string {
	return integrationPath(integrationID) + "/builds"
}

func buildPath(integrationID, buildName string) string {
	return buildsPath(integrationID) + "/" + url.PathEscape(buildName)
}

// buildListRecovery is where to find a build's name.
func buildListRecovery(integrationID string) string {
	return "Run wso2 intg integration build list " + integrationID + " to see the names of its builds."
}

// buildRunFlags are the options of "wso2 intg integration build run".
type buildRunFlags struct {
	commit string
	waitFlags
}

// buildRun answers "wso2 intg integration build run <integration> [--commit <sha>]".
//
// The API accepts the request without naming the build it started, so the
// result names the listing where it appears, newest first. With --wait the
// listing is read before the build is started, the build is found as the
// name that was not there before, and it is followed until it finishes.
func buildRun(command *cobra.Command, flags *buildRunFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		integrationID, err := exactlyOneArgument(command.Flags().Args(), "name the integration to build",
			"Run wso2 intg integration build run <integration> [--commit <sha>].")
		if err != nil {
			return result.Result{}, err
		}
		body := map[string]string{}
		commit := "branch head"
		if flags.commit != "" {
			if !fullCommitSHA.MatchString(flags.commit) {
				return result.Result{}, usageProblem("--commit "+flags.commit+" is not a full 40-character commit SHA, "+
					"and a build can fetch only a full one", "Give the full SHA, or leave --commit out to build the "+
					"head of the integration's branch.")
			}
			body["sha"] = strings.ToLower(flags.commit)
			commit = body["sha"]
		}
		if err := flags.validate(); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var before map[string]bool
		if flags.wait {
			listed, err := listBuilds(ctx, client, integrationID, request.Context.Endpoint)
			if err != nil {
				return result.Result{}, err
			}
			before = map[string]bool{}
			for _, b := range listed {
				before[b.Name] = true
			}
		}
		var accepted struct {
			Message string `json:"message"`
		}
		if err := client.Post(ctx, buildsPath(integrationID), body, &accepted); err != nil {
			if refused, ok := refusal(err, http.StatusBadGateway); ok {
				return result.Result{}, problem.New(problem.CategoryProductService, "intg.call_failed",
					"the Integration Platform could not start a build of "+integrationID+": "+refusalWords(refused)).
					WithRecovery("Run wso2 intg integration build run " + integrationID + " again.")
			}
			return result.Result{}, callFailedNaming(err, "start a build of "+integrationID, request.Context.Endpoint,
				integrationListRecovery(""))
		}
		if flags.wait {
			return waitForBuild(ctx, client, integrationID, body["sha"], before, flags.timeout, request.Context.Endpoint)
		}
		return result.New(BuildStartSchema).
			With("integration", "Integration", integrationID).
			With("commit", "Commit", commit).
			With("message", "Message", orDash(accepted.Message)).
			With(NextField, "Next", "Run wso2 intg integration build list "+integrationID+" to see it; the newest build is "+
				"listed first."), nil
	}
}

// buildList answers "wso2 intg integration build list <integration>".
//
// The API returns at most 20 builds, newest first, with no continuation
// cursor.
func buildList(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		integrationID, err := exactlyOneArgument(command.Flags().Args(), "name the integration whose builds to list",
			"Run wso2 intg integration build list <integration>.")
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		builds, err := listBuilds(ctx, client, integrationID, request.Context.Endpoint)
		if err != nil {
			return result.Result{}, err
		}
		report := result.New(BuildsSchema).
			With("integration", "Integration", integrationID).
			With("count", "Builds", strconv.Itoa(len(builds))).
			WithColumn("name", "Name").
			WithColumn("status", "Status").
			WithColumn("trigger", "Trigger").
			WithColumn("commit", "Commit").
			WithColumn("started", "Started").
			WithColumn("completed", "Completed")
		for _, b := range builds {
			report = report.WithRow(b.Name, orDash(b.Status), orDash(b.Trigger), b.shortCommit(), orDash(b.Started),
				orDash(b.Completed))
		}
		next := "Run wso2 intg integration build show " + integrationID + " <build> to see one build."
		if len(builds) == 0 {
			next = "It has no builds yet. Start one with wso2 intg integration build run " + integrationID + "."
		}
		return report.With(NextField, "Next", next), nil
	}
}

// buildShow answers "wso2 intg integration build show <integration> <build>".
func buildShow(command *cobra.Command) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		integrationID, buildName, err := exactlyTwoArguments(command.Flags().Args(),
			"name the integration and the build to show", "Run wso2 intg integration build show <integration> <build>.")
		if err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var found build
		if err := client.Get(ctx, buildPath(integrationID, buildName), &found); err != nil {
			return result.Result{}, buildReadFailed(err, "show the build "+buildName+" of "+integrationID,
				request.Context.Endpoint, integrationID)
		}
		if found.Name == "" {
			found.Name = buildName
		}
		if found.Integration == "" {
			found.Integration = integrationID
		}
		next := "Run wso2 intg integration build logs " + integrationID + " " + found.Name + " to see its output."
		switch found.Status {
		case "Failed":
			next = "Run wso2 intg integration build logs " + integrationID + " " + found.Name + " to see why it failed."
		case "Pending", "Running":
			next = "Run wso2 intg integration build show " + integrationID + " " + found.Name + " again to follow it."
		}
		return buildReport(found, next), nil
	}
}

// buildReport is one build in display order.
func buildReport(found build, next string) result.Result {
	return result.New(BuildSchema).
		With("name", "Name", found.Name).
		With("integration", "Integration", found.Integration).
		With("project", "Project", orDash(found.Project)).
		With("status", "Status", orDash(found.Status)).
		With("trigger", "Trigger", orDash(found.Trigger)).
		With("commit", "Commit", orDash(found.Commit)).
		With("image", "Image", orDash(found.Image)).
		With("started", "Started", orDash(found.Started)).
		With("completed", "Completed", orDash(found.Completed)).
		With("steps", "Steps", found.steps()).
		With(NextField, "Next", next)
}

// listBuilds reads an integration's recent builds, newest first.
func listBuilds(ctx context.Context, client platform.Client, integrationID, endpoint string) ([]build, error) {
	var listing struct {
		Items []build `json:"items"`
	}
	if err := client.Get(ctx, buildsPath(integrationID), &listing); err != nil {
		return nil, callFailedNaming(err, "list the builds of "+integrationID, endpoint, integrationListRecovery(""))
	}
	return listing.Items, nil
}

// waitForBuild finds the build a request just started and follows it until it
// finishes.
//
// The API does not name the build it started, so it is found in the listing
// as a build that was not there before, of the commit asked for when one was.
// Someone else can start a build of the same integration at the same moment,
// and nothing then tells the two apart: rather than follow either, the wait
// refuses with intg.build_ambiguous, naming both.
func waitForBuild(ctx context.Context, client platform.Client, integrationID, commit string, before map[string]bool,
	limit time.Duration, endpoint string) (result.Result, error) {
	deadline := time.Now().Add(limit)
	var found build
	err := poll(ctx, limit, func() (bool, error) {
		listed, err := listBuilds(ctx, client, integrationID, endpoint)
		if err != nil {
			return false, err
		}
		var started []build
		for _, b := range listed {
			if !before[b.Name] && (commit == "" || strings.EqualFold(b.Commit, commit)) {
				started = append(started, b)
			}
		}
		switch len(started) {
		case 0:
			return false, nil
		case 1:
			found = started[0]
			return true, nil
		default:
			names := make([]string, len(started))
			for i, b := range started {
				names[i] = b.Name
			}
			return false, problem.New(problem.CategoryProductService, "intg.build_ambiguous",
				"more than one build of "+integrationID+" started at once ("+strings.Join(names, ", ")+
					"), so the one this command started cannot be told apart").
				WithRecovery("Run wso2 intg integration build show " + integrationID + " <build> for each to " +
					"follow it, or start the build with --commit <sha> to tell it apart.")
		}
	})
	if errors.Is(err, errWaitTimeout) {
		return result.Result{}, problem.New(problem.CategoryProductService, "intg.wait_timeout",
			"the build of "+integrationID+" did not appear within "+limit.String()).
			WithRecovery("Run wso2 intg integration build list " + integrationID + " to see whether it started.")
	}
	if err != nil {
		return result.Result{}, err
	}
	err = poll(ctx, time.Until(deadline), func() (bool, error) {
		if err := client.Get(ctx, buildPath(integrationID, found.Name), &found); err != nil {
			return false, buildReadFailed(err, "follow the build "+found.Name+" of "+integrationID, endpoint, integrationID)
		}
		return !buildInProgress(found.Status), nil
	})
	if errors.Is(err, errWaitTimeout) {
		return result.Result{}, problem.New(problem.CategoryProductService, "intg.wait_timeout",
			"the build "+found.Name+" of "+integrationID+" was still "+orDash(found.Status)+" after "+limit.String()).
			WithRecovery("Run wso2 intg integration build show " + integrationID + " " + found.Name + " to follow it.")
	}
	if err != nil {
		return result.Result{}, err
	}
	if found.Integration == "" {
		found.Integration = integrationID
	}
	if found.Status != "Succeeded" {
		return result.Result{}, problem.New(problem.CategoryProductService, "intg.build_failed",
			"the build "+found.Name+" of "+integrationID+" finished as "+found.Status).
			WithRecovery("Run wso2 intg integration build logs " + integrationID + " " + found.Name + " to see why.")
	}
	return buildReport(found, "Run wso2 intg integration deploy "+integrationID+" --env <environment> --build "+
		found.Name+" to deploy it."), nil
}

// buildReadFailed maps a refused read of one build. The API answers a build
// name it does not know with a 500 rather than a 404, so a 500 here points at
// the listing too.
func buildReadFailed(err error, attempted, endpoint, integrationID string) error {
	if refused, ok := refusal(err, http.StatusInternalServerError); ok {
		return problem.New(problem.CategoryProductService, "intg.call_failed",
			"the Integration Platform could not "+attempted+": "+refusalWords(refused)).
			WithRecovery("Check the build name: " + buildListRecovery(integrationID) + " If the name is right, retry " +
				"in a moment.")
	}
	return callFailedNaming(err, attempted, endpoint, buildListRecovery(integrationID))
}

// buildLogsFlags are the options of "wso2 intg integration build logs".
type buildLogsFlags struct {
	tail int
}

// logsQueryPath is the log query of WSO2 Cloud's observability proxy,
// relative to the gateway URL a context records for this product (for
// example https://cloud-obs-proxy.obs.dp.<domain>/wso2cloud-obs). It is the
// request the Integration Platform console makes for a build's log.
//
// The proxy is called directly, as the WSO2 Cloud team asked: the API's own
// build-log route and its /observability pass-through are being removed.
const logsQueryPath = "/api/v1/logs/query"

// logsQueryLimit is the most lines one build log is read with, the same bound
// the API's own route uses.
const logsQueryLimit = 1000

// logWindowMargin widens a build's run on both sides, so lines stamped just
// before it started or after it completed are not cut off.
const logWindowMargin = 5 * time.Minute

// logWindow is the time range to search for a build's log: its run, widened
// by logWindowMargin, up to now while it has not completed, and the last 30
// days when it states no start.
func (b build) logWindow(now time.Time) (time.Time, time.Time) {
	start, err := time.Parse(time.RFC3339, b.Started)
	if err != nil {
		return now.Add(-30 * 24 * time.Hour), now
	}
	end := now
	if completed, err := time.Parse(time.RFC3339, b.Completed); err == nil {
		end = completed.Add(logWindowMargin)
	}
	return start.Add(-logWindowMargin), end
}

// requireObservability refuses a command that reads logs when the context
// records no observability proxy, before anything is asked of the shell.
func requireObservability(request module.Request) error {
	if request.Context.GatewayEndpoint != "" {
		return nil
	}
	return problem.New(problem.CategoryUsage, "intg.observability_not_recorded",
		"the selected context records no observability URL for the intg product, so logs have nowhere to be "+
			"read from").
		WithRecovery("Run wso2 context product add intg --url <integration platform API URL> --gateway " +
			"<observability URL> --replace to record it, then run this command again.")
}

// queryLogs runs one log query against the observability proxy for scope,
// between start and end, and returns the lines oldest first with the total
// the proxy matched. A tail below the limit asks for the newest lines first,
// so a long log is not cut off before its end, and puts them back in order.
//
// The proxy sets searchScope.namespace from the token's organization, so the
// member is sent empty. It is presented the token the API is called with: it
// verifies the token's signature and organization claims, and checks no
// audience.
func queryLogs(ctx context.Context, request module.Request, token string, scope map[string]string,
	start, end time.Time, tail int, attempted string) ([]string, int, error) {
	order, limit := "asc", logsQueryLimit
	if tail > 0 && tail < logsQueryLimit {
		order, limit = "desc", tail
	}
	searchScope := map[string]string{"namespace": ""}
	for key, value := range scope {
		searchScope[key] = value
	}
	query := map[string]any{
		"searchScope": searchScope,
		"startTime":   start.UTC().Format(time.RFC3339),
		"endTime":     end.UTC().Format(time.RFC3339),
		"limit":       limit,
		"sortOrder":   order,
	}
	var answer struct {
		Logs []struct {
			Timestamp string `json:"timestamp"`
			Log       string `json:"log"`
		} `json:"logs"`
		Total int `json:"total"`
	}
	observability := platform.Client{Endpoint: request.Context.GatewayEndpoint, Token: token}
	if err := observability.Post(ctx, logsQueryPath, query, &answer); err != nil {
		return nil, 0, callFailed(err, attempted, request.Context.GatewayEndpoint)
	}
	lines := make([]string, len(answer.Logs))
	for i, entry := range answer.Logs {
		line := strings.TrimRight(entry.Log, "\n")
		if entry.Timestamp != "" {
			line = entry.Timestamp + " " + line
		}
		lines[i] = line
	}
	if order == "desc" {
		slices.Reverse(lines)
	}
	return lines, answer.Total, nil
}

// lineCount is how many lines a log result shows, and of how many when the
// proxy matched more.
func lineCount(shown, total int) string {
	count := strconv.Itoa(shown)
	if total > shown {
		count += " of " + strconv.Itoa(total)
	}
	return count
}

// buildLogs answers "wso2 intg integration build logs <integration> <build> [--tail <n>]".
//
// The build is read first, which confirms it exists and bounds the search to
// its run. The log is then queried from the observability proxy, recorded on
// the context as the product's gateway, as the workflow run of that name.
func buildLogs(command *cobra.Command, flags *buildLogsFlags) module.Handler {
	return func(ctx context.Context, request module.Request) (result.Result, error) {
		integrationID, buildName, err := exactlyTwoArguments(command.Flags().Args(),
			"name the integration and the build whose log to show", "Run wso2 intg integration build logs <integration> <build>.")
		if err != nil {
			return result.Result{}, err
		}
		if flags.tail < 0 {
			return result.Result{}, usageProblem("--tail "+strconv.Itoa(flags.tail)+" is not a number of lines",
				"Give --tail a positive number, or leave it out to see the whole log.")
		}
		if err := requireObservability(request); err != nil {
			return result.Result{}, err
		}
		client, err := clientFor(ctx, request)
		if err != nil {
			return result.Result{}, err
		}
		var found build
		if err := client.Get(ctx, buildPath(integrationID, buildName), &found); err != nil {
			return result.Result{}, buildReadFailed(err, "find the build "+buildName+" of "+integrationID,
				request.Context.Endpoint, integrationID)
		}
		start, end := found.logWindow(time.Now().UTC())
		lines, total, err := queryLogs(ctx, request, client.Token, map[string]string{"workflowRunName": buildName},
			start, end, flags.tail, "read the log of the build "+buildName+" of "+integrationID)
		if err != nil {
			return result.Result{}, err
		}
		next := "Run wso2 intg integration build show " + integrationID + " " + buildName + " to see its status."
		if len(lines) == 0 {
			next = "There is no output yet; it appears once the build runs. Run wso2 intg integration build show " +
				integrationID + " " + buildName + " to see its status."
		}
		return result.New(BuildLogsSchema).
			With("build", "Build", buildName).
			With("integration", "Integration", integrationID).
			With("lines", "Lines", lineCount(len(lines), total)).
			With("log", "Log", orDash(strings.Join(lines, "\n"))).
			With(NextField, "Next", next), nil
	}
}
