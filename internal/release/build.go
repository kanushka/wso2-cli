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

package release

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/wso2/wso2-cli/internal/install"
	"github.com/wso2/wso2-cli/internal/modules"
)

// Build compiles one module's executable for one platform and reports its
// bytes.
//
// Every caller that produces a module executable comes here, because a module
// built one way for a release and another way for a developer would differ in
// the ways hardest to notice: a missing injected version installs and then
// refuses to launch on the placeholder the receipt does not match, and a
// missing build flag changes the binary without changing anything a test looks
// at. Keeping one build means the loop a developer runs is the loop that
// publishes.
//
// The version and the SDK version are injected rather than left at their
// development placeholders. The module announces both at the handshake, the
// shell compares the first against the receipt, and the second is what a module
// tells the shell about its own build.
func Build(moduleDir, namespace, version string, platform modules.Platform) ([]byte, error) {
	sdkVersion, err := SDKVersion(moduleDir)
	if err != nil {
		return nil, err
	}

	// A private directory rather than a predictable name under os.TempDir():
	// two runs on one machine would otherwise build over each other, and a
	// pre-existing file or symlink at that path would be followed.
	workDir, err := os.MkdirTemp("", "wso2-module-build-")
	if err != nil {
		return nil, fmt.Errorf("release: creating a build directory for %s failed: %w", platform, err)
	}
	defer func() {
		_ = os.RemoveAll(workDir)
	}()

	output := filepath.Join(workDir, install.ExecutableName(namespace, platform))
	// The compiler's own output is what says which line failed, so it is
	// carried into the error rather than left on a stream the caller may not be
	// showing.
	var failure bytes.Buffer
	command := exec.Command("go", "build", "-trimpath",
		"-ldflags", BuildFlags(version, sdkVersion), "-o", output, MainPackage(namespace))
	command.Dir = moduleDir
	command.Env = append(os.Environ(),
		"CGO_ENABLED=0",
		"GOOS="+platform.OS,
		"GOARCH="+platform.Arch,
		"GOARM=6",
	)
	command.Stderr = &failure
	if err := command.Run(); err != nil {
		return nil, fmt.Errorf("release: building the %s module for %s failed: %w\n%s",
			namespace, platform, err, failure.String())
	}

	built, err := os.ReadFile(output)
	if err != nil {
		return nil, fmt.Errorf("release: reading the built %s module failed: %w", namespace, err)
	}
	return built, nil
}
