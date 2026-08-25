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

// Command wso2-catalog-input assembles the catalog generator's input from the
// tags that exist and what each one's release published.
//
//	go run ./cmd/wso2-catalog-input -out releases.json | go run ./cmd/wso2-catalog ...
//
// It is the wiring between a release and the catalog: the module tags come
// from git, the compatibility and capabilities each tag declared come from the
// module declaration as it stood at that tag, and each artifact's URL, size,
// and digest are read back from the release that published it rather than from
// the build that produced it. Nothing is curated and nothing is carried by
// hand, so regenerating over an unchanged tag set reproduces the same input.
//
// It reads the release page through the gh command line, which the workflow
// authenticates with the run's own token.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/wso2/wso2-cli/internal/catalog"
	"github.com/wso2/wso2-cli/internal/release"
)

// sdkTagPrefix is the SDK's own tag namespace. The SDK is not a product module
// and publishes no module archives, so its tags are not module tags. Every
// other prefixed tag is a module tag and is required to name a buildable
// module, which is what keeps a mistyped namespace from being ignored.
const sdkTagPrefix = "sdk/"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-catalog-input: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	repositoryRoot := flag.String("repo", ".", "Repository checkout to read tags and declarations from.")
	outputPath := flag.String("out", "releases.json", "Path to write the assembled input document to.")
	flag.Parse()

	declarations, err := catalog.Discover(*repositoryRoot)
	if err != nil {
		return err
	}
	input, err := release.AssembleInput(publishedReleases{repositoryRoot: *repositoryRoot}, declarations)
	if err != nil {
		return err
	}
	document, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Errorf("rendering the input document failed: %w", err)
	}
	if err := os.WriteFile(*outputPath, append(document, '\n'), 0o644); err != nil {
		return fmt.Errorf("writing %s failed: %w", *outputPath, err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "assembled %d module tag(s) into %s\n", len(input.Tags), *outputPath)
	return nil
}

// publishedReleases reads the released world from git and the release page.
type publishedReleases struct {
	repositoryRoot string
}

func (p publishedReleases) ModuleTags() ([]string, error) {
	listed, err := p.git("tag", "--list", "*/v*")
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, tag := range strings.Fields(listed) {
		if strings.HasPrefix(tag, sdkTagPrefix) {
			continue
		}
		tags = append(tags, tag)
	}
	return tags, nil
}

func (p publishedReleases) DeclarationAt(tag, directory string) (catalog.Declaration, error) {
	path := directory + "/" + catalog.DeclarationFileName
	content, stderr, err := p.gitCapture("show", tag+":"+path)
	if err != nil {
		if pathMissingAtRef(stderr) {
			fmt.Fprintf(os.Stderr, "wso2-catalog-input: %s does not exist at %s; "+
				"excluding %s from the catalog as an invalid release\n", path, tag, tag)
			return catalog.Declaration{}, fmt.Errorf("%w: %s at %s", release.ErrDeclarationMissing, path, tag)
		}
		return catalog.Declaration{}, fmt.Errorf("reading %s at %s failed: %w", path, tag, err)
	}
	var declaration catalog.Declaration
	if err := json.Unmarshal([]byte(content), &declaration); err != nil {
		return catalog.Declaration{}, fmt.Errorf("%s at %s is not a readable module declaration: %w",
			path, tag, err)
	}
	return declaration, nil
}

func (p publishedReleases) PublishedAt(tag string) (release.Published, error) {
	listed, err := p.gh("release", "view", tag, "--json", "assets")
	if err != nil {
		return release.Published{}, fmt.Errorf("reading the release for %s failed: %w", tag, err)
	}
	var view struct {
		Assets []release.Asset `json:"assets"`
	}
	if err := json.Unmarshal([]byte(listed), &view); err != nil {
		return release.Published{}, fmt.Errorf("the release for %s is not readable: %w", tag, err)
	}

	digests, err := p.digestsAt(tag)
	if err != nil {
		return release.Published{}, err
	}
	return release.Published{Assets: view.Assets, Digests: digests}, nil
}

// digestsAt reads the checksum file the release published. The digests come
// from that file rather than from the release API, which reports none, and a
// release without one publishes nothing a download could be verified against.
func (p publishedReleases) digestsAt(tag string) (map[string]string, error) {
	directory, err := os.MkdirTemp("", "wso2-catalog-input")
	if err != nil {
		return nil, fmt.Errorf("creating a temporary directory failed: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(directory)
	}()

	if _, err := p.gh("release", "download", tag,
		"--pattern", release.ChecksumsFileName, "--dir", directory, "--clobber"); err != nil {
		return nil, fmt.Errorf("the release for %s published no %s: %w",
			tag, release.ChecksumsFileName, err)
	}
	content, err := os.ReadFile(filepath.Join(directory, release.ChecksumsFileName))
	if err != nil {
		return nil, fmt.Errorf("reading the checksum file for %s failed: %w", tag, err)
	}

	digests := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		// sha256sum writes a binary-mode entry as "*name".
		digests[strings.TrimPrefix(fields[1], "*")] = fields[0]
	}
	if len(digests) == 0 {
		return nil, fmt.Errorf("the checksum file for %s covers nothing", tag)
	}
	return digests, nil
}

func (p publishedReleases) git(arguments ...string) (string, error) {
	return p.command("git", arguments...)
}

func (p publishedReleases) gh(arguments ...string) (string, error) {
	return p.command("gh", arguments...)
}

func (p publishedReleases) command(name string, arguments ...string) (string, error) {
	command := exec.Command(name, arguments...)
	command.Dir = p.repositoryRoot
	command.Stderr = os.Stderr
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("%s %s failed: %w", name, strings.Join(arguments, " "), err)
	}
	return string(output), nil
}

// gitCapture runs a git command, reporting its standard error alongside the
// usual output-or-error so a caller can tell one failure reason from another.
// The standard error still reaches the console through the tee, so nothing
// that would have appeared in the log is lost by capturing it.
func (p publishedReleases) gitCapture(arguments ...string) (stdout, stderr string, err error) {
	command := exec.Command("git", arguments...)
	command.Dir = p.repositoryRoot
	var errBuffer bytes.Buffer
	command.Stderr = io.MultiWriter(os.Stderr, &errBuffer)
	output, err := command.Output()
	if err != nil {
		return "", errBuffer.String(),
			fmt.Errorf("git %s failed: %w", strings.Join(arguments, " "), err)
	}
	return string(output), errBuffer.String(), nil
}

// pathMissingAtRef reports whether git's standard error says the path being
// read does not exist at the ref, as opposed to some other failure (a network
// error, an unreadable object, an unauthenticated remote) that a tag being
// genuinely invalid would not explain.
func pathMissingAtRef(stderr string) bool {
	return strings.Contains(stderr, "does not exist in") ||
		strings.Contains(stderr, "exists on disk, but not in")
}
