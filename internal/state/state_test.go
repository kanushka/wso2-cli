package state

import (
	"path/filepath"
	"testing"
)

func TestRootPrefersTheOverrideEnvironmentVariable(t *testing.T) {
	isolated := t.TempDir()
	t.Setenv(RootEnvVar, isolated)

	root, err := Root()
	if err != nil {
		t.Fatalf("Root returned %v", err)
	}
	if root != isolated {
		t.Fatalf("Root() = %q, want the isolated override %q", root, isolated)
	}
}

func TestRootFallsBackToTheUserHomeDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv(RootEnvVar, "")
	t.Setenv("HOME", home)

	root, err := Root()
	if err != nil {
		t.Fatalf("Root returned %v", err)
	}
	if want := filepath.Join(home, ".wso2"); root != want {
		t.Fatalf("Root() = %q, want %q", root, want)
	}
}

func TestRootRejectsARelativeOverrideRatherThanGuessing(t *testing.T) {
	t.Setenv(RootEnvVar, "relative/state")

	if _, err := Root(); err == nil {
		t.Fatal("Root accepted a relative override; an ambiguous state root must fail closed")
	}
}

func TestModuleStorePathsAreDerivedFromTheRoot(t *testing.T) {
	store := ModuleStore("/isolated")

	if want := filepath.Join("/isolated", "cli", "modules"); store != want {
		t.Fatalf("ModuleStore = %q, want %q", store, want)
	}
}
