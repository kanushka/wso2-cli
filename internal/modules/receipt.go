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
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// ReceiptSchemaVersion is the only module-receipt schema this shell reads. An
// unknown schema version fails closed rather than being partially interpreted.
const ReceiptSchemaVersion = 1

// ReceiptFileName is the receipt's fixed name inside a module version
// directory.
const ReceiptFileName = "receipt.json"

// namespacePattern constrains a product namespace to a single lowercase
// command word. It also keeps a namespace usable as one path element.
var namespacePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,31}$`)

// ValidNamespace reports whether a string is a well-formed product namespace.
// The catalog generator applies the same rule the shell applies, so a namespace
// the shell would refuse cannot be published in the first place.
func ValidNamespace(namespace string) bool {
	return namespacePattern.MatchString(namespace)
}

// Receipt is shell-owned local metadata that identifies one installed module
// executable and carries every fact needed to resolve it without executing it.
//
// A receipt records integrity, compatibility, and declared capability facts. It
// is not a publisher or release verification record: the architecture proof
// establishes executable integrity only.
type Receipt struct {
	// SchemaVersion identifies the receipt format.
	SchemaVersion int `json:"schemaVersion"`
	// Namespace is the top-level command name this module owns.
	Namespace string `json:"namespace"`
	// ModuleVersion is the installed module's own release version.
	ModuleVersion string `json:"moduleVersion"`
	// Executable is the module executable's path relative to its immutable
	// version directory. It must not escape that directory.
	Executable string `json:"executable"`
	// Compatibility records the shell and protocol versions the module
	// supports.
	Compatibility Compatibility `json:"compatibility"`
	// Platform records the operating system and architecture the installed
	// executable was built for.
	Platform Platform `json:"platform"`
	// Capabilities records the access the module is permitted to request.
	Capabilities Capabilities `json:"capabilities"`
	// ExecutableSHA256 is the hex-encoded SHA-256 digest of the executable,
	// recomputed before every launch.
	ExecutableSHA256 string `json:"executableSha256"`
}

// Compatibility declares the shell and protocol versions a module supports.
type Compatibility struct {
	// Shell is a conjunctive semantic-version range such as ">=0.1.0 <1.0.0".
	Shell string `json:"shell"`
	// ProtocolVersions are the module-contract versions the module speaks.
	ProtocolVersions []int `json:"protocolVersions"`
}

// Platform is the operating system and architecture pair an executable targets.
type Platform struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
}

// String renders the platform in the "os/arch" form used in user output.
func (p Platform) String() string {
	return p.OS + "/" + p.Arch
}

// Capabilities are the module's declared authentication requests. The broker
// intersects a runtime request with these values, so a module cannot escalate
// beyond what its receipt declares.
type Capabilities struct {
	AuthAudiences []string `json:"authAudiences,omitempty"`
	AuthScopes    []string `json:"authScopes,omitempty"`
}

// ShellRange parses the receipt's shell compatibility range.
func (r Receipt) ShellRange() (semver.Range, error) {
	return semver.ParseRange(r.Compatibility.Shell)
}

// SupportsProtocol reports whether the module speaks the given protocol version.
func (r Receipt) SupportsProtocol(version int) bool {
	for _, supported := range r.Compatibility.ProtocolVersions {
		if supported == version {
			return true
		}
	}
	return false
}

// Validate reports whether the receipt is internally well formed. It does not
// consult the filesystem or this shell's own versions; ReadReceipt and Resolve
// layer those checks on top.
func (r Receipt) Validate() error {
	if r.SchemaVersion != ReceiptSchemaVersion {
		return receiptProblem("modules.receipt_schema_unsupported",
			fmt.Sprintf("module receipt schema version %d is not supported by this shell", r.SchemaVersion),
			"Reinstall the module with a shell that owns this receipt schema.")
	}
	if !namespacePattern.MatchString(r.Namespace) {
		return receiptProblem("modules.receipt_malformed",
			fmt.Sprintf("module receipt declares an invalid namespace %q", r.Namespace),
			reinstallRecovery)
	}
	if _, err := semver.Parse(r.ModuleVersion); err != nil {
		return receiptProblem("modules.receipt_malformed",
			fmt.Sprintf("module receipt declares an invalid module version %q", r.ModuleVersion),
			reinstallRecovery)
	}
	if err := validateRelativeExecutable(r.Executable); err != nil {
		return err
	}
	if _, err := r.ShellRange(); err != nil {
		return receiptProblem("modules.receipt_malformed",
			fmt.Sprintf("module receipt declares an unreadable shell compatibility range %q", r.Compatibility.Shell),
			reinstallRecovery)
	}
	if len(r.Compatibility.ProtocolVersions) == 0 {
		return receiptProblem("modules.receipt_malformed",
			"module receipt declares no supported protocol version",
			reinstallRecovery)
	}
	for _, version := range r.Compatibility.ProtocolVersions {
		if version <= 0 {
			return receiptProblem("modules.receipt_malformed",
				fmt.Sprintf("module receipt declares an invalid protocol version %d", version),
				reinstallRecovery)
		}
	}
	if r.Platform.OS == "" || r.Platform.Arch == "" {
		return receiptProblem("modules.receipt_malformed",
			"module receipt does not declare an operating system and architecture",
			reinstallRecovery)
	}
	if err := validateDigest(r.ExecutableSHA256); err != nil {
		return err
	}
	return nil
}

func validateRelativeExecutable(executable string) error {
	if executable == "" {
		return receiptProblem("modules.receipt_malformed",
			"module receipt does not declare an executable",
			reinstallRecovery)
	}
	if filepath.IsAbs(executable) || strings.HasPrefix(executable, "/") || strings.HasPrefix(executable, `\`) {
		return receiptProblem("modules.receipt_path_escape",
			fmt.Sprintf("module receipt declares an absolute executable path %q", executable),
			pathEscapeRecovery)
	}
	// A drive-relative path such as "c:module" is not absolute by Go's
	// definition, so it would survive the check above and then name a volume
	// rather than a path inside the version directory. Only paths relative to
	// that directory are accepted.
	if hasVolumeName(executable) {
		return receiptProblem("modules.receipt_path_escape",
			fmt.Sprintf("module receipt declares a volume-relative executable path %q", executable),
			pathEscapeRecovery)
	}
	// Reject an escaping path before any filesystem access, so a receipt can
	// never redirect execution outside its own immutable version directory.
	cleaned := filepath.Clean(filepath.FromSlash(executable))
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return receiptProblem("modules.receipt_path_escape",
			fmt.Sprintf("module receipt declares an executable path %q outside its version directory", executable),
			pathEscapeRecovery)
	}
	return nil
}

// hasVolumeName reports whether the path begins with a Windows volume name,
// such as "c:" or `\\host\share`. The check runs on every platform so a receipt
// is rejected consistently wherever it is inspected.
func hasVolumeName(path string) bool {
	if strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, "//") {
		return true
	}
	if len(path) >= 2 && path[1] == ':' {
		letter := path[0]
		return ('a' <= letter && letter <= 'z') || ('A' <= letter && letter <= 'Z')
	}
	return false
}

func validateDigest(digest string) error {
	if len(digest) != sha256.Size*2 {
		return receiptProblem("modules.receipt_malformed",
			"module receipt does not declare a SHA-256 executable digest",
			reinstallRecovery)
	}
	if _, err := hex.DecodeString(digest); err != nil {
		return receiptProblem("modules.receipt_malformed",
			"module receipt declares an unreadable executable digest",
			reinstallRecovery)
	}
	return nil
}

// ReadReceipt loads and validates the receipt at the given path.
func ReadReceipt(path string) (Receipt, error) {
	data, err := os.ReadFile(path)
	switch {
	case os.IsNotExist(err):
		return Receipt{}, receiptProblem("modules.receipt_missing",
			"the installed module has no receipt", reinstallRecovery)
	case err != nil:
		return Receipt{}, receiptProblem("modules.receipt_unreadable",
			"the module receipt cannot be read", reinstallRecovery)
	}
	return DecodeReceipt(data)
}

// DecodeReceipt parses and validates receipt bytes.
//
// Unknown JSON members are tolerated so a newer shell can add receipt facts
// within the same schema version, but a trailing document or an unknown schema
// version fails closed.
func DecodeReceipt(data []byte) (Receipt, error) {
	var receipt Receipt
	if err := decodeOneDocument(data, &receipt); err != nil {
		return Receipt{}, receiptProblem("modules.receipt_malformed",
			"the module receipt "+err.Error(), reinstallRecovery)
	}
	if err := receipt.Validate(); err != nil {
		return Receipt{}, err
	}
	return receipt, nil
}

// Encode renders the receipt as the canonical on-disk document.
func (r Receipt) Encode() ([]byte, error) {
	return encodeDocument(r)
}

// decodeOneDocument parses exactly one JSON document into target. A trailing
// document is rejected so a receipt or pointer cannot smuggle a second value
// past a decoder that stops after the first.
func decodeOneDocument(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(target); err != nil {
		return errors.New("is not valid JSON")
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("contains more than one JSON document")
	}
	return nil
}

// encodeDocument renders one shell-owned state document.
func encodeDocument(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("modules: cannot encode %T: %w", value, err)
	}
	return append(data, '\n'), nil
}

const (
	reinstallRecovery  = "Reinstall the module so the shell can resolve a valid receipt."
	pathEscapeRecovery = "Reinstall the module. The shell only launches executables inside a module's own version directory."
)

func receiptProblem(code, message, recovery string) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, code, message).WithRecovery(recovery)
}
