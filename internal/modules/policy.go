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

package modules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/wso2/wso2-cli/sdk/problem"
)

// PolicySchemaVersion is the only version-policy schema this shell reads.
const PolicySchemaVersion = 1

// PolicyFileName is the version policy's fixed name inside a namespace
// directory.
const PolicyFileName = "policy.json"

// Policy is what the user asked for about one module's versions, kept beside
// that module's installations.
//
// It is recorded per namespace rather than once for the shell, because a user
// takes a prerelease of one product without taking prereleases of all of them,
// and because a pin is a property of the module that is held rather than of the
// run that held it. Keeping it on disk is what makes a pin survive an update
// run: an update reads the policy it was installed under rather than being told
// again on the command line.
type Policy struct {
	SchemaVersion int    `json:"schemaVersion"`
	Namespace     string `json:"namespace"`
	// Channel is the release channel this module follows. An empty channel is
	// the stable one.
	Channel string `json:"channel,omitempty"`
	// PinnedVersion holds this module at one exact version. While it is set, an
	// update run passes the module over rather than moving it.
	PinnedVersion string `json:"pinnedVersion,omitempty"`
}

// Pinned reports whether the module is held at an exact version.
func (p Policy) Pinned() bool {
	return p.PinnedVersion != ""
}

// FollowedChannel reports the channel the module follows, resolving an
// unrecorded channel to the stable one.
func (p Policy) FollowedChannel() string {
	if p.Channel == "" {
		return ChannelStable
	}
	return p.Channel
}

// ChannelStable is the channel a module follows when none was chosen. The
// catalog's channel names are the same strings; this shell-side copy exists so
// reading local state does not depend on the catalog package.
const ChannelStable = "stable"

// PolicyPath reports the version policy path for one namespace.
func (s Store) PolicyPath(namespace string) string {
	return filepath.Join(s.NamespaceDir(namespace), PolicyFileName)
}

// ReadPolicy loads the version policy of one namespace. A namespace with no
// recorded policy follows the stable channel and is not pinned, which is what
// a module installed before any policy was written follows too.
func (s Store) ReadPolicy(namespace string) (Policy, error) {
	if !namespacePattern.MatchString(namespace) {
		return Policy{}, problem.New(problem.CategoryUsage, "modules.invalid_namespace",
			fmt.Sprintf("%q is not a valid module namespace", namespace)).
			WithRecovery("Run wso2 version to see the installed modules.")
	}

	data, err := os.ReadFile(s.PolicyPath(namespace))
	switch {
	case os.IsNotExist(err):
		return Policy{SchemaVersion: PolicySchemaVersion, Namespace: namespace}, nil
	case err != nil:
		return Policy{}, policyMalformed(namespace, "cannot be read")
	}

	var policy Policy
	if err := decodeOneDocument(data, &policy); err != nil {
		return Policy{}, policyMalformed(namespace, err.Error())
	}
	if policy.SchemaVersion != PolicySchemaVersion {
		return Policy{}, problem.New(problem.CategoryModuleTrust, "modules.policy_schema_unsupported",
			fmt.Sprintf("version policy schema version %d is not supported by this shell", policy.SchemaVersion)).
			WithRecovery("Reinstall the module with a shell that owns this policy schema.")
	}
	if policy.Namespace != namespace {
		return Policy{}, policyMalformed(namespace, fmt.Sprintf("names another namespace %q", policy.Namespace))
	}
	if policy.PinnedVersion != "" && !isVersionDirName(policy.PinnedVersion) {
		return Policy{}, policyMalformed(namespace, fmt.Sprintf("pins an invalid version %q", policy.PinnedVersion))
	}
	return policy, nil
}

// Encode renders the version policy as the canonical on-disk document.
func (p Policy) Encode() ([]byte, error) {
	return encodeDocument(p)
}

func policyMalformed(namespace, detail string) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, "modules.policy_malformed",
		fmt.Sprintf("the version policy of the %q module %s", namespace, detail)).
		WithRecovery("Reinstall the module, choosing the channel or the version you want it to follow.")
}
