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

package acceptance_test

import (
	"archive/zip"
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
	"github.com/wso2/wso2-cli/internal/statusservice"
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// The previous half of the protocol window is a promise to users who have not
// updated the shell yet, and scripts/previous-protocol.sh is the gate that
// proves it on every pull request. This file holds the two halves the gate
// cannot express in shell: the conformance run it launches, and the proof that
// the resolution it performs works before any SDK is published.

// Environment the gate hands the conformance run. It names a module the gate
// already built against a published SDK, rather than a protocol version to
// build one at, because building it from this checkout is exactly what the
// gate exists to avoid.
const (
	previousModuleVariable   = "WSO2_PREVIOUS_PROTOCOL_MODULE"
	previousSDKVariable      = "WSO2_PREVIOUS_PROTOCOL_SDK"
	previousProtocolVariable = "WSO2_PREVIOUS_PROTOCOL_VERSION"
)

// TestAModuleBuiltAgainstThePreviousProtocolSDKRunsUnderThisShell is the
// conformance run. The module is a subprocess built elsewhere, against a
// published SDK with the workspace dropped, and the shell is the one this
// branch builds with the window it declares: the test kit would prove the
// SDK's own server loop rather than the shell's handshake across a generation.
func TestAModuleBuiltAgainstThePreviousProtocolSDKRunsUnderThisShell(t *testing.T) {
	modulePath := os.Getenv(previousModuleVariable)
	if modulePath == "" {
		t.Skipf("%s is unset: this run is launched by scripts/previous-protocol.sh, "+
			"which builds the module against the published SDK first",
			previousModuleVariable)
	}
	sdkVersion := os.Getenv(previousSDKVariable)
	protocolVersion, err := strconv.Atoi(os.Getenv(previousProtocolVariable))
	if err != nil {
		t.Fatalf("%s is not a protocol version: %v", previousProtocolVariable, err)
	}

	// The failure a broken previous protocol produces has to name both sides,
	// so the reader learns which generation broke and which release it was
	// measured against rather than that some test failed.
	subject := "protocol v" + strconv.Itoa(protocolVersion) + ", SDK " + sdkVersion

	if window := protocol.Window(); len(window) < 2 || window[1] != protocolVersion {
		t.Fatalf("%s: the shell's window is %v, which does not contain the protocol "+
			"the module was built against", subject, window)
	}

	// The receipt is written from what the module says about itself rather than
	// from constants here. A module built against a published SDK carries its
	// own versions, and reading them back is also how this run proves the SDK
	// that was resolved really is the older generation.
	identity := referenceModuleIdentity(t, modulePath)
	if len(identity.ProtocolVersions) != 1 || identity.ProtocolVersions[0] != protocolVersion {
		t.Fatalf("%s: the module speaks protocol %v, so the resolved SDK is not the "+
			"one that generation was released from", subject, identity.ProtocolVersions)
	}

	shell := buildShellSpeaking(t, "")
	stateRoot := isolatedStateRoot(t)
	if _, err := fixture.Install(state.ModuleStore(stateRoot), fixture.Module{
		Namespace:        identity.Namespace,
		Version:          identity.Version,
		ProtocolVersions: identity.ProtocolVersions,
		SourcePath:       modulePath,
		AuthAudiences:    []string{referenceAudience},
		AuthScopes:       []string{referenceReadScope},
	}); err != nil {
		t.Fatalf("%s: fixture.Install returned %v", subject, err)
	}
	deployment := deployInstalled(t, stateRoot, statusservice.Options{})

	stdout, stderr, err := deployment.try(shell, "reference", "status")

	if err != nil {
		t.Fatalf("%s: wso2 reference status failed: %v\nstdout:\n%s\nstderr:\n%s",
			subject, err, stdout, stderr)
	}
	if !strings.Contains(stdout, "operational") {
		t.Fatalf("%s: wso2 reference status did not report the service:\n%s",
			subject, stdout)
	}
	if stderr != "" {
		t.Errorf("%s: wso2 reference status wrote diagnostics:\n%s", subject, stderr)
	}
}

// TestThePreviousProtocolGateResolvesThePublishedSDKItsGenerationNames proves
// the gate's mechanism now, against a module proxy this test publishes to,
// because no SDK release exists yet and an unexercised gate proves nothing on
// the day one does.
//
// The proxy carries two releases: one speaking the previous protocol and a
// newer one speaking the current protocol. Resolving the older of the two is
// the whole behaviour — the gate has to choose by protocol generation, not by
// taking the newest release it can see.
func TestThePreviousProtocolGateResolvesThePublishedSDKItsGenerationNames(t *testing.T) {
	if testing.Short() {
		t.Skip("the gate builds three modules and launches a shell")
	}
	window := protocol.Window()
	if len(window) < 2 {
		t.Skip("this branch declares the first protocol generation, which has no predecessor")
	}
	current, previous := window[0], window[1]

	proxy := t.TempDir()
	const wanted = fixturePreviousVersion
	publishSDK(t, proxy, wanted, previous)
	publishSDK(t, proxy, fixtureCurrentVersion, current)

	output := runGate(t, "GOPROXY=file://"+filepath.ToSlash(proxy)+
		",https://proxy.golang.org,direct", "GOSUMDB=off")

	if !strings.Contains(output, "Resolved SDK "+wanted) {
		t.Fatalf("the gate did not resolve SDK %s for protocol v%d:\n%s",
			wanted, previous, output)
	}
	if !strings.Contains(output, "PASSED") {
		t.Fatalf("the gate did not pass against a published SDK:\n%s", output)
	}
}

// TestBreakingThePreviousProtocolFailsTheGate is the criterion the gate exists
// for. The breakage is a plausible one rather than a contrived one: a shell
// that negotiates only the newest version of its own window still declares the
// window, still passes every test that reads the declaration, and stops
// launching every module one generation behind.
//
// It is injected into a scratch checkout, because a gate is only worth having
// if something that should fail it does.
func TestBreakingThePreviousProtocolFailsTheGate(t *testing.T) {
	if testing.Short() {
		t.Skip("the gate builds three modules and launches a shell")
	}
	window := protocol.Window()
	if len(window) < 2 {
		t.Skip("this branch declares the first protocol generation, which has no predecessor")
	}

	clone := cloneRepository(t)
	proxy := t.TempDir()
	const sdkVersion = fixturePreviousVersion
	publishSDK(t, proxy, sdkVersion, window[1])
	breakPreviousProtocol(t, clone)

	command := gateCommand(t, clone)
	command.Env = append(command.Env,
		"GOPROXY=file://"+filepath.ToSlash(proxy)+",https://proxy.golang.org,direct",
		"GOSUMDB=off")
	combined, err := command.CombinedOutput()
	output := string(combined)

	if err == nil {
		t.Fatalf("the gate passed against a shell that refuses the previous protocol:\n%s",
			output)
	}
	// A generic CI failure would leave the reader guessing at which of the two
	// moving parts broke, so the gate's own conclusion names both. The
	// assertion is on that conclusion rather than on the whole log, where the
	// two versions appear in the progress lines whatever the outcome.
	start := strings.LastIndex(output, "FAILED:")
	if start < 0 {
		t.Fatalf("the gate failed without stating a conclusion:\n%s", output)
	}
	conclusion := output[start:]
	for _, want := range []string{"protocol v" + strconv.Itoa(window[1]), "SDK " + sdkVersion} {
		if !strings.Contains(conclusion, want) {
			t.Errorf("the gate's conclusion does not name %q:\n%s", want, conclusion)
		}
	}
}

// breakPreviousProtocol narrows the shell's negotiation to the newest version
// of its window without touching the window itself.
func breakPreviousProtocol(t *testing.T, clone string) {
	t.Helper()
	path := filepath.Join(clone, "internal", "modules", "resolve.go")
	source, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading the resolver failed: %v", err)
	}
	const negotiation = "for _, candidate := range shell.ProtocolVersions {"
	if !strings.Contains(string(source), negotiation) {
		t.Fatalf("the resolver no longer negotiates with %q", negotiation)
	}
	broken := strings.Replace(string(source), negotiation,
		"for _, candidate := range shell.ProtocolVersions[:1] {", 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatalf("writing the broken resolver failed: %v", err)
	}
}

// TestAProxyThatCannotBeAskedFailsTheGate pins the difference between "nothing
// is published" and "nobody could be asked". The first is the honest state of
// this repository before its first SDK release; the second is a run that proved
// nothing, and reading it as the first would turn a network blip into a green
// gate.
func TestAProxyThatCannotBeAskedFailsTheGate(t *testing.T) {
	if testing.Short() {
		t.Skip("the gate builds the SDK to read the protocol it declares")
	}
	if len(protocol.Window()) < 2 {
		t.Skip("this branch declares the first protocol generation, which has no predecessor")
	}

	command := gateCommand(t, cloneRepository(t))
	command.Env = append(command.Env, "GOPROXY=off")
	output, err := command.CombinedOutput()

	if err == nil {
		t.Fatalf("the gate passed without reaching a module proxy:\n%s", output)
	}
	if !strings.Contains(string(output), "FAILED") {
		t.Errorf("the gate did not report the proxy as a failure:\n%s", output)
	}
}

// A protocol generation that never had an SDK release has nothing in the field
// built against it, so there is nothing for this gate to prove. That is not the
// same as a generation whose release is missing: a published version cannot be
// withdrawn, so a generation that was ever released stays resolvable and stays
// enforced. Reporting the empty premise as a failure would make the gate red for
// a reason no change can fix, which is how a gate gets switched off.
func TestAGenerationThatWasNeverPublishedIsNotEnforceable(t *testing.T) {
	if testing.Short() {
		t.Skip("the gate builds the SDK to read the protocol it declares")
	}
	window := protocol.Window()
	if len(window) < 2 {
		t.Skip("this branch declares the first protocol generation, which has no predecessor")
	}

	// A proxy publishing the current generation only, which is what the first
	// SDK release of a repository looks like.
	//
	// The version is deliberately one no real SDK release will ever carry. A
	// fixture publishes with GOSUMDB=off, so its synthetic zip enters the module
	// cache under whatever version it names: reusing a version that is really
	// published would leave a poisoned entry there, and the next build to want
	// the real one fails verification against the checksum database. See the
	// note on fixtureVersion.
	proxy := t.TempDir()
	publishSDK(t, proxy, fixtureCurrentVersion, window[0])

	output := runGate(t, "GOPROXY=file://"+filepath.ToSlash(proxy)+",off", "GOSUMDB=off")

	if !strings.Contains(output, "NOT ENFORCEABLE") {
		t.Fatalf("the gate did not report the generation as unenforceable:\n%s", output)
	}
	// The versions it examined have to be named, so a reader can tell an empty
	// premise from a gate that failed to look.
	if !strings.Contains(output, fixtureCurrentVersion) {
		t.Errorf("the gate does not name the published versions it examined:\n%s", output)
	}
	if strings.Contains(output, "FAILED") {
		t.Errorf("the gate reported a failure for an empty premise:\n%s", output)
	}
}

// TestThePreviousProtocolGateRefusesACommittedReplaceDirective proves the
// prohibition on committed replace directives is enforced rather than
// documented. The directive is written into a scratch checkout, so nothing in
// this repository gains one.
func TestThePreviousProtocolGateRefusesACommittedReplaceDirective(t *testing.T) {
	if testing.Short() {
		t.Skip("the check runs against a scratch clone of the repository")
	}
	clone := cloneRepository(t)

	edit := exec.Command("go", "mod", "edit", "-replace",
		"github.com/wso2/wso2-cli/sdk=./sdk", "modules/reference/go.mod")
	edit.Dir = clone
	if combined, err := edit.CombinedOutput(); err != nil {
		t.Fatalf("adding a replace directive failed: %v\n%s", err, combined)
	}

	output, err := gateCommand(t, clone).CombinedOutput()

	if err == nil {
		t.Fatalf("the gate accepted a committed replace directive:\n%s", output)
	}
	if !strings.Contains(string(output), "modules/reference/go.mod") {
		t.Errorf("the gate did not name the offending go.mod:\n%s", output)
	}
}

// runGate runs the gate in a scratch clone of this checkout with the given
// extra environment. A clone rather than the checkout itself, because the gate
// writes nothing but a test wanting to break something has to be able to.
func runGate(t *testing.T, environment ...string) string {
	t.Helper()
	command := gateCommand(t, cloneRepository(t))
	command.Env = append(command.Env, environment...)
	combined, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("scripts/previous-protocol.sh failed: %v\n%s", err, combined)
	}
	return string(combined)
}

func gateCommand(t *testing.T, directory string) *exec.Cmd {
	t.Helper()
	command := exec.Command("./scripts/previous-protocol.sh")
	command.Dir = directory
	// The workspace is left in place deliberately: the gate drops it exactly
	// where a released module's graph is being reproduced, and the shell it
	// builds is composed from the workspace like any other build here.
	command.Env = os.Environ()
	return command
}

// cloneRepository copies the working tree into a temporary directory, with a
// git repository of its own because the gate reads the repository's go.mod
// files through git.
func cloneRepository(t *testing.T) string {
	t.Helper()
	clone := t.TempDir()
	root := repoRoot(t)
	// Everything git would track, including files not committed yet: a gate
	// still being written has to be runnable by the test that proves it.
	archive := exec.Command("sh", "-c",
		"git ls-files -co --exclude-standard -z | tar -c --null -T - | tar -x -C "+
			strconv.Quote(clone))
	archive.Dir = root
	if combined, err := archive.CombinedOutput(); err != nil {
		t.Fatalf("copying the checkout failed: %v\n%s", err, combined)
	}
	for _, arguments := range [][]string{
		{"init", "--quiet"},
		{"-c", "user.email=gate@example.invalid", "-c", "user.name=gate",
			"commit", "--quiet", "--allow-empty", "-m", "checkout"},
	} {
		command := exec.Command("git", arguments...)
		command.Dir = clone
		if combined, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %s failed: %v\n%s", arguments[0], err, combined)
		}
	}
	add := exec.Command("git", "add", "--all")
	add.Dir = clone
	if combined, err := add.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, combined)
	}
	return clone
}

// publishSDK writes this checkout's SDK to a file-backed module proxy at one
// version, speaking one protocol generation. It is how a released SDK is
// reproduced without releasing one: the source is the same, and only the
// protocol generation it declares differs.
// The SDK versions the fixtures publish.
//
// None of them is a version the SDK really publishes, and that is a rule rather
// than a coincidence. A fixture serves its own zip from a file proxy with
// GOSUMDB=off, and the module cache keys that zip by the version it names. A
// fixture that reused a real published version would leave the cache holding
// bytes the checksum database does not vouch for, and the next build in the same
// job that wanted the real module would fail verification — a failure whose
// message points at the module rather than at the fixture that poisoned it.
const (
	fixturePreviousVersion = "v1.4.0"
	fixtureCurrentVersion  = "v1.5.0"
)

// TestNoFixtureVersionCollidesWithThePublishedSDK is the rule above, enforced.
//
// It failed once as a checksum mismatch on the real module, in a job where a
// fixture had published its own zip under a version the SDK really carries. The
// message named the module and the checksum database, and said nothing about the
// fixture, so the same mistake would be as expensive to find the second time.
func TestNoFixtureVersionCollidesWithThePublishedSDK(t *testing.T) {
	required := sdkVersionRequiredBy(t, filepath.Join(repoRoot(t), "modules", "reference"))

	for _, fixture := range []string{fixturePreviousVersion, fixtureCurrentVersion} {
		if fixture == required {
			t.Errorf("the fixture version %s is the version the reference module requires; "+
				"a fixture must name a version no real release carries", fixture)
		}
	}
}

// sdkVersionRequiredBy reports the SDK version one module requires, read from
// its module graph.
func sdkVersionRequiredBy(t *testing.T, moduleDir string) string {
	t.Helper()
	command := exec.Command("go", "mod", "edit", "-json")
	command.Dir = moduleDir
	command.Env = os.Environ()
	output, err := command.Output()
	if err != nil {
		t.Fatalf("reading the module graph of %s failed: %v", moduleDir, err)
	}
	var parsed struct {
		Require []struct {
			Path    string `json:"Path"`
			Version string `json:"Version"`
		} `json:"Require"`
	}
	if err := json.Unmarshal(output, &parsed); err != nil {
		t.Fatalf("cannot read the module graph of %s: %v", moduleDir, err)
	}
	for _, requirement := range parsed.Require {
		if requirement.Path == "github.com/wso2/wso2-cli/sdk" {
			return requirement.Version
		}
	}
	t.Fatalf("%s requires no SDK version", moduleDir)
	return ""
}

func publishSDK(t *testing.T, proxy, version string, protocolVersion int) {
	t.Helper()
	const modulePath = "github.com/wso2/wso2-cli/sdk"
	directory := filepath.Join(proxy, filepath.FromSlash(modulePath), "@v")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("creating the proxy directory failed: %v", err)
	}

	source := filepath.Join(repoRoot(t), "sdk")
	goMod, err := os.ReadFile(filepath.Join(source, "go.mod"))
	if err != nil {
		t.Fatalf("reading the SDK go.mod failed: %v", err)
	}
	write := func(name string, content []byte) {
		if err := os.WriteFile(filepath.Join(directory, name), content, 0o644); err != nil {
			t.Fatalf("writing %s failed: %v", name, err)
		}
	}
	write(version+".mod", goMod)
	write(version+".info", []byte(`{"Version":"`+version+`","Time":"2026-01-01T00:00:00Z"}`))
	appendLine(t, filepath.Join(directory, "list"), version)

	archive, err := os.Create(filepath.Join(directory, version+".zip"))
	if err != nil {
		t.Fatalf("creating the module zip failed: %v", err)
	}
	defer func() { _ = archive.Close() }()
	writer := zip.NewWriter(archive)
	prefix := modulePath + "@" + version + "/"
	err = filepath.WalkDir(source, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if relative == filepath.Join("protocol", "protocol.go") {
			content = declareProtocol(t, content, protocolVersion)
		}
		file, err := writer.Create(prefix + filepath.ToSlash(relative))
		if err != nil {
			return err
		}
		_, err = file.Write(content)
		return err
	})
	if err != nil {
		t.Fatalf("packaging the SDK failed: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("closing the module zip failed: %v", err)
	}
}

// declareProtocol rewrites the SDK's one protocol declaration, which is how a
// release of an older generation is reproduced. The ldflag the acceptance
// helpers use cannot serve here: this SDK is consumed as a dependency, and the
// gate builds it with its own flags.
func declareProtocol(t *testing.T, source []byte, version int) []byte {
	t.Helper()
	const declaration = `var Version = "`
	start := strings.Index(string(source), declaration)
	if start < 0 {
		t.Fatalf("the SDK no longer declares the protocol version as %q", declaration)
	}
	end := strings.Index(string(source[start+len(declaration):]), `"`)
	if end < 0 {
		t.Fatal("the SDK's protocol version declaration is unterminated")
	}
	return []byte(string(source[:start+len(declaration)]) + strconv.Itoa(version) +
		string(source[start+len(declaration)+end:]))
}

func appendLine(t *testing.T, path, line string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("opening %s failed: %v", path, err)
	}
	defer func() { _ = file.Close() }()
	if _, err := io.WriteString(file, line+"\n"); err != nil {
		t.Fatalf("writing %s failed: %v", path, err)
	}
}
