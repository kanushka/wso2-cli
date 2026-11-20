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

// Command wso2-bundle builds test bundles: for each platform, a zip holding a
// shell built from this checkout, one product module packed the way a release
// packs it, and the installer that sets both up on a machine that has neither
// this repository nor a Go toolchain. Nothing is published: a tester downloads
// the zip, unpacks it and runs its installer.
//
// Run it from the repository root:
//
//	go run ./cmd/wso2-bundle -namespace intg -context modules/intg/dev/wso2cloud-dev.yaml \
//	    -context-name wso2cloud-dev
package main

import (
	"archive/zip"
	"crypto/sha256"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/wso2/wso2-cli/internal/bundle"
	"github.com/wso2/wso2-cli/internal/devorigin"
	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/release"
	"github.com/wso2/wso2-cli/internal/semver"
)

// defaultPlatforms are the platforms a bundle is built for unless -platforms
// names others: the desktops a tester is likely to have.
const defaultPlatforms = "darwin/arm64,darwin/amd64,linux/amd64,linux/arm64,windows/amd64"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "wso2-bundle: %v\n", err)
		os.Exit(1)
	}
}

type options struct {
	namespace     string
	moduleVersion string
	shellVersion  string
	commandName   string
	platforms     []modules.Platform
	contextFile   string
	contextName   string
	out           string
}

func run() error {
	stamp := time.Now().UTC().Format("200601021504")
	namespace := flag.String("namespace", "", "The product module to bundle, such as intg. Required.")
	moduleVersion := flag.String("module-version", "0.1.0-test."+stamp, "The version to build the module as.")
	shellVersion := flag.String("shell-version", "0.1.3-test."+stamp, "The version to build the shell as.")
	commandName := flag.String("command-name", "ws", "The shell's program name.")
	platforms := flag.String("platforms", defaultPlatforms, "Comma-separated os/arch pairs to build for.")
	contextFile := flag.String("context", "", "An input file of contexts the installer applies, if any.")
	contextName := flag.String("context-name", "", "The context in -context the installer selects.")
	out := flag.String("out", filepath.Join("dist", "bundles"), "Where to write the zips.")
	flag.Parse()

	if *namespace == "" {
		flag.Usage()
		return fmt.Errorf("-namespace is required")
	}
	if (*contextFile == "") != (*contextName == "") {
		return fmt.Errorf("-context and -context-name are given together or not at all")
	}
	parsed, err := parsePlatforms(*platforms)
	if err != nil {
		return err
	}
	return build(options{
		namespace: *namespace, moduleVersion: *moduleVersion, shellVersion: *shellVersion,
		commandName: *commandName, platforms: parsed, contextFile: *contextFile, contextName: *contextName,
		out: *out,
	})
}

func parsePlatforms(list string) ([]modules.Platform, error) {
	var platforms []modules.Platform
	for _, entry := range strings.Split(list, ",") {
		osName, arch, ok := strings.Cut(strings.TrimSpace(entry), "/")
		if !ok || osName == "" || arch == "" {
			return nil, fmt.Errorf("-platforms entry %q is not os/arch", entry)
		}
		platforms = append(platforms, modules.Platform{OS: osName, Arch: arch})
	}
	return platforms, nil
}

func build(o options) error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "go.work")); err != nil {
		return fmt.Errorf("run wso2-bundle from the repository root, where go.work is")
	}
	if err := os.MkdirAll(o.out, 0o755); err != nil {
		return err
	}
	work, err := os.MkdirTemp("", "wso2-bundle-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(work) }()

	var sums []string
	for _, platform := range o.platforms {
		name := fmt.Sprintf("wso2-%s-%s-%s-%s", o.namespace, o.moduleVersion, platform.OS, platform.Arch)
		dir := filepath.Join(work, name)
		if err := buildOne(root, dir, o, platform); err != nil {
			return fmt.Errorf("%s/%s: %w", platform.OS, platform.Arch, err)
		}
		zipped := filepath.Join(o.out, name+".zip")
		if err := zipDir(dir, name, zipped); err != nil {
			return err
		}
		sum, err := fileSHA256(zipped)
		if err != nil {
			return err
		}
		sums = append(sums, sum+"  "+name+".zip")
		fmt.Printf("Wrote %s\n", zipped)
	}
	sort.Strings(sums)
	checksums := filepath.Join(o.out, "checksums-"+o.moduleVersion+".txt")
	if err := os.WriteFile(checksums, []byte(strings.Join(sums, "\n")+"\n"), 0o644); err != nil {
		return err
	}
	fmt.Printf("Wrote %s\n", checksums)
	return nil
}

func buildOne(root, dir string, o options, platform modules.Platform) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	declaration, archive, err := devorigin.Pack(root, o.namespace, o.moduleVersion, platform)
	if err != nil {
		return err
	}
	// The installer would refuse a shell the module's range leaves out, on the
	// tester's machine; it is refused here instead, before anything ships.
	supported, err := semver.ParseRange(declaration.Compatibility.Shell)
	if err != nil {
		return err
	}
	shellVersion, err := semver.Parse(o.shellVersion)
	if err != nil {
		return fmt.Errorf("-shell-version %q is not a semantic version: %w", o.shellVersion, err)
	}
	if !supported.Contains(shellVersion) {
		return fmt.Errorf("the %s module runs under a shell matching %q, and -shell-version is %s",
			o.namespace, declaration.Compatibility.Shell, o.shellVersion)
	}

	archiveName := release.ArchiveName(declaration.Namespace, o.moduleVersion, platform)
	if err := os.WriteFile(filepath.Join(dir, archiveName), archive, 0o644); err != nil {
		return err
	}
	if err := goBuild(root, platform, filepath.Join(dir, bundle.ExecutableName(o.commandName, platform)),
		"-ldflags", "-X github.com/wso2/wso2-cli/internal/version.shellVersion="+o.shellVersion+
			" -X github.com/wso2/wso2-cli/internal/output.commandName="+o.commandName,
		"./cmd/wso2"); err != nil {
		return err
	}
	if err := goBuild(root, platform, filepath.Join(dir, bundle.ExecutableName(bundle.InstallerName, platform)),
		"./cmd/wso2-bundle-install"); err != nil {
		return err
	}
	declared := filepath.Join(dir, filepath.FromSlash(declaration.Directory), "module.json")
	if err := copyFile(filepath.Join(root, filepath.FromSlash(declaration.Directory), "module.json"), declared); err != nil {
		return err
	}
	manifest := bundle.Manifest{
		Namespace: o.namespace, ModuleVersion: o.moduleVersion, ShellVersion: o.shellVersion,
		CommandName: o.commandName, Platform: platform, Archive: archiveName,
		ContextName: o.contextName,
	}
	if o.contextFile != "" {
		manifest.ContextFile = filepath.Base(o.contextFile)
		if err := copyFile(o.contextFile, filepath.Join(dir, manifest.ContextFile)); err != nil {
			return err
		}
	}
	if err := manifest.Write(dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "README.txt"), []byte(readme(manifest)), 0o644)
}

func goBuild(root string, platform modules.Platform, output string, args ...string) error {
	command := exec.Command("go", append([]string{"build", "-trimpath", "-o", output}, args...)...)
	command.Dir = root
	command.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS="+platform.OS, "GOARCH="+platform.Arch)
	if message, err := command.CombinedOutput(); err != nil {
		return fmt.Errorf("go build %s: %w\n%s", args[len(args)-1], err, message)
	}
	return nil
}

func copyFile(source, destination string) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, content, 0o644)
}

// zipDir packs dir into a zip whose entries sit under prefix, keeping each
// executable's mode so it unpacks runnable on macOS and Linux.
func zipDir(dir, prefix, destination string) error {
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	defer func() { _ = file.Close() }()
	writer := zip.NewWriter(file)
	err = filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = prefix + "/" + filepath.ToSlash(relative)
		header.Method = zip.Deflate
		target, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		source, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = source.Close() }()
		_, err = io.Copy(target, source)
		return err
	})
	if err != nil {
		return err
	}
	return writer.Close()
}

func fileSHA256(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func readme(m bundle.Manifest) string {
	installer := "./" + bundle.InstallerName
	shell := m.CommandName
	var platformNotes string
	switch m.Platform.OS {
	case "darwin":
		platformNotes = "macOS blocks programs downloaded from the internet that are not signed.\n" +
			"Before the first run, clear that flag on the unpacked folder:\n\n" +
			"    xattr -dr com.apple.quarantine .\n\n" +
			"Login stores its session in the macOS Keychain.\n"
	case "linux":
		platformNotes = "Login stores its session in the desktop keyring (Secret Service, such as\n" +
			"GNOME Keyring), and opens a browser. A machine without either cannot log in.\n"
	case "windows":
		installer = ".\\" + bundle.ExecutableName(bundle.InstallerName, m.Platform)
		platformNotes = "Run the commands below in PowerShell. Windows may warn that the programs are\n" +
			"unsigned; choose to run them anyway. Login stores its session in the Windows\n" +
			"Credential Manager.\n"
	}
	return fmt.Sprintf(`WSO2 CLI test bundle: %[1]s %[2]s, with the shell %[3]s %[4]s, for %[5]s/%[6]s

This is a test build made from a checkout, not a release. It installs
the shell and the %[1]s product module into your WSO2 state root (~/.wso2, or
WSO2_HOME when set). Installing over an existing ~/.wso2 replaces the %[3]s
program there; set WSO2_HOME to a directory of its own to keep them apart.

%[7]s
Install, from this folder:

    %[8]s

The installer prints the line that puts %[3]s on PATH. Then:

    %[9]s login
    %[9]s %[1]s project list
    %[9]s %[1]s --help

Run the installer again from a newer bundle to update. Your contexts and login
are kept.
`, m.Namespace, m.ModuleVersion, m.CommandName, m.ShellVersion, m.Platform.OS, m.Platform.Arch,
		platformNotes, installer, shell)
}
