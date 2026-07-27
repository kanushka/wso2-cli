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

// Package module is the author-facing entry point of the public WSO2 CLI SDK.
//
// A product module declares its identity here and implements product commands.
// The wire protocol, framing, authentication broker exchange, output
// rendering, and exit codes are not the module's concern.
//
// The architecture proof establishes the identity and build boundaries only.
// Protocol serving is added by the next slice increment, so this package
// deliberately exposes no Serve function yet.
package module

import (
	"github.com/wso2/wso2-cli/sdk/protocol"
)

// SDKVersion is the version of this SDK release.
//
// It is a build-time variable so a module build can pin and report the SDK it
// was built against, independently of the shell version. Override it with:
//
//	-ldflags "-X github.com/wso2/wso2-cli/sdk/module.SDKVersion=0.2.0"
var SDKVersion = "0.0.0-dev"

// Descriptor is a module's runtime identity. The shell compares it with the
// module receipt before a product operation proceeds, so a descriptor must
// describe the executable that is actually running.
type Descriptor struct {
	// Namespace is the single top-level command name this module owns.
	Namespace string `json:"namespace"`
	// Version is the module's own release version.
	Version string `json:"version"`
	// SDKVersion is the SDK release the module was built against.
	SDKVersion string `json:"sdkVersion"`
	// ProtocolVersions are the module-contract versions the module can
	// speak, newest first.
	ProtocolVersions []int `json:"protocolVersions"`
	// AuthAudiences are the access audiences the module may request.
	AuthAudiences []string `json:"authAudiences,omitempty"`
	// AuthScopes are the access scopes the module may request.
	AuthScopes []string `json:"authScopes,omitempty"`
}

// Options describe a module to the SDK. A module author supplies its namespace,
// version, and declared access requests; the SDK supplies protocol and SDK
// versions so those cannot drift from the linked SDK.
type Options struct {
	Namespace     string
	Version       string
	AuthAudiences []string
	AuthScopes    []string
}

// Describe builds the runtime identity for a module.
func Describe(options Options) Descriptor {
	return Descriptor{
		Namespace:        options.Namespace,
		Version:          options.Version,
		SDKVersion:       SDKVersion,
		ProtocolVersions: protocol.Supported(),
		AuthAudiences:    append([]string(nil), options.AuthAudiences...),
		AuthScopes:       append([]string(nil), options.AuthScopes...),
	}
}
