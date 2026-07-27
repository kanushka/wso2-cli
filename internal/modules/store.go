package modules

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wso2/wso2-cli/internal/semver"
	"github.com/wso2/wso2-cli/sdk/problem"
)

// ActiveSchemaVersion is the only active-version pointer schema this shell
// reads.
const ActiveSchemaVersion = 1

// ActiveFileName is the active-version pointer's fixed name inside a
// namespace directory.
const ActiveFileName = "active.json"

// VersionsDirName is the fixed directory holding one immutable directory per
// installed module version.
const VersionsDirName = "versions"

// Active is the pointer that selects one installed version of a module.
//
// The pointer is a state file rather than a symlink so behaviour is identical
// on Windows, and it records the receipt digest so an activated version cannot
// be silently substituted.
type Active struct {
	SchemaVersion int    `json:"schemaVersion"`
	Namespace     string `json:"namespace"`
	Version       string `json:"version"`
	ReceiptSHA256 string `json:"receiptSha256"`
}

// Store is the shell-owned managed module store: the only place the shell
// resolves module executables from. It never consults PATH or the working
// directory.
type Store struct {
	root string
}

// NewStore opens the managed module store rooted at the given directory. The
// directory is the store itself, not the surrounding state root; use
// state.ModuleStore to derive it.
func NewStore(root string) Store {
	return Store{root: root}
}

// Root reports the store's root directory.
func (s Store) Root() string {
	return s.root
}

// NamespaceDir reports the directory owning one namespace's installations.
func (s Store) NamespaceDir(namespace string) string {
	return filepath.Join(s.root, namespace)
}

// VersionDir reports the immutable directory holding one installed version.
func (s Store) VersionDir(namespace, version string) string {
	return filepath.Join(s.NamespaceDir(namespace), VersionsDirName, version)
}

// ReceiptPath reports the receipt path for one installed version.
func (s Store) ReceiptPath(namespace, version string) string {
	return filepath.Join(s.VersionDir(namespace, version), ReceiptFileName)
}

// ActivePath reports the active-version pointer path for one namespace.
func (s Store) ActivePath(namespace string) string {
	return filepath.Join(s.NamespaceDir(namespace), ActiveFileName)
}

// Namespaces lists the namespaces present in the store, sorted. A missing
// store is not an error: a shell with no installed module is a valid state.
func (s Store) Namespaces() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("modules: cannot read the managed module store: %w", err)
	}

	var namespaces []string
	for _, entry := range entries {
		if !entry.IsDir() || !namespacePattern.MatchString(entry.Name()) {
			continue
		}
		namespaces = append(namespaces, entry.Name())
	}
	sort.Strings(namespaces)
	return namespaces, nil
}

// ReadActive loads and validates the active-version pointer for a namespace.
func (s Store) ReadActive(namespace string) (Active, error) {
	data, err := os.ReadFile(s.ActivePath(namespace))
	switch {
	case os.IsNotExist(err):
		return Active{}, problem.New(problem.CategoryModuleTrust, "modules.no_active_version",
			fmt.Sprintf("no version of the %q module is active", namespace)).
			WithRecovery("Install the module so the shell can activate one version.")
	case err != nil:
		return Active{}, problem.New(problem.CategoryModuleTrust, "modules.active_unreadable",
			fmt.Sprintf("the active-version pointer for the %q module cannot be read", namespace)).
			WithRecovery(reinstallRecovery)
	}

	var active Active
	if err := decodeOneDocument(data, &active); err != nil {
		return Active{}, activeMalformed(namespace, err.Error())
	}
	if active.SchemaVersion != ActiveSchemaVersion {
		return Active{}, problem.New(problem.CategoryModuleTrust, "modules.active_schema_unsupported",
			fmt.Sprintf("active-version pointer schema version %d is not supported by this shell", active.SchemaVersion)).
			WithRecovery("Reinstall the module with a shell that owns this pointer schema.")
	}
	if active.Namespace != namespace {
		return Active{}, activeMalformed(namespace, fmt.Sprintf("names another namespace %q", active.Namespace))
	}
	if !isVersionDirName(active.Version) {
		return Active{}, activeMalformed(namespace, fmt.Sprintf("names an invalid version %q", active.Version))
	}
	if err := validateDigest(active.ReceiptSHA256); err != nil {
		return Active{}, activeMalformed(namespace, "does not record a SHA-256 receipt digest")
	}
	return active, nil
}

// Encode renders the active-version pointer as the canonical on-disk document.
func (a Active) Encode() ([]byte, error) {
	return encodeDocument(a)
}

// isVersionDirName reports whether a version string is safe to use as one path
// element of the managed store. It rejects separators and relative elements so
// an active pointer cannot select a directory outside the namespace.
func isVersionDirName(version string) bool {
	if version == "" || version == "." || version == ".." {
		return false
	}
	if version != filepath.Base(version) || filepath.IsAbs(version) {
		return false
	}
	if _, err := semver.Parse(version); err != nil {
		return false
	}
	return true
}

func activeMalformed(namespace, detail string) problem.Problem {
	return problem.New(problem.CategoryModuleTrust, "modules.active_malformed",
		fmt.Sprintf("the active-version pointer for the %q module %s", namespace, detail)).
		WithRecovery(reinstallRecovery)
}
