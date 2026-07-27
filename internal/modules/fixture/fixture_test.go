package fixture_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wso2/wso2-cli/internal/modules"
	"github.com/wso2/wso2-cli/internal/modules/fixture"
	"github.com/wso2/wso2-cli/internal/state"
)

func TestInstallCreatesAnIsolatedStoreWithAReceiptAndActivePointer(t *testing.T) {
	storeRoot := filepath.Join(t.TempDir(), "cli", "modules")

	receipt, err := fixture.Install(storeRoot, fixture.Module{
		Namespace:     "reference",
		Version:       "0.1.0",
		ShellRange:    ">=0.1.0 <1.0.0",
		AuthAudiences: []string{"reference-status"},
		AuthScopes:    []string{"reference:status:read"},
	})
	if err != nil {
		t.Fatalf("Install returned %v", err)
	}

	store := modules.NewStore(storeRoot)
	if err := receipt.Validate(); err != nil {
		t.Fatalf("the installed receipt is invalid: %v", err)
	}
	if receipt.ExecutableSHA256 == "" {
		t.Fatal("the installed receipt records no executable digest")
	}

	executable := filepath.Join(store.VersionDir("reference", "0.1.0"), "wso2-module-reference")
	digest, err := modules.FileDigest(executable)
	if err != nil {
		t.Fatalf("cannot digest the installed executable: %v", err)
	}
	if digest != receipt.ExecutableSHA256 {
		t.Fatal("the receipt digest does not match the installed executable")
	}

	active, err := store.ReadActive("reference")
	if err != nil {
		t.Fatalf("ReadActive returned %v", err)
	}
	if active.Version != "0.1.0" {
		t.Fatalf("active version = %q, want 0.1.0", active.Version)
	}
}

func TestInstallRefusesToTouchRealWSO2UserState(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no user home directory available: %v", err)
	}

	for _, root := range []string{
		filepath.Join(home, ".wso2"),
		filepath.Join(home, ".wso2", "cli", "modules"),
	} {
		_, err := fixture.Install(root, fixture.Module{Namespace: "reference", Version: "0.1.0"})
		if err == nil {
			t.Fatalf("Install accepted the real state path %q", root)
		}
		if !strings.Contains(err.Error(), "refusing to install into WSO2 state") {
			t.Fatalf("Install(%q) returned %v, want a refusal naming real WSO2 state", root, err)
		}
	}
}

func TestInstallRefusesToTouchTheStateRootSelectedByTheEnvironment(t *testing.T) {
	// A developer whose environment points at real state elsewhere is
	// protected too, not only the default ~/.wso2 location.
	configured := t.TempDir()
	t.Setenv(state.RootEnvVar, configured)

	_, err := fixture.Install(state.ModuleStore(configured), fixture.Module{Namespace: "reference", Version: "0.1.0"})
	if err == nil {
		t.Fatal("Install accepted the state root the environment selects")
	}
	if !strings.Contains(err.Error(), "refusing to install into WSO2 state") {
		t.Fatalf("Install returned %v, want a refusal naming WSO2 state", err)
	}
}

func TestInstallRequiresAnAbsoluteStoreRoot(t *testing.T) {
	if _, err := fixture.Install("relative/store", fixture.Module{Namespace: "reference", Version: "0.1.0"}); err == nil {
		t.Fatal("Install accepted a relative store root")
	}
}
