package module

import (
	"reflect"
	"testing"
)

func TestDescribeReportsSDKOwnedProtocolAndSDKVersions(t *testing.T) {
	descriptor := Describe(Options{
		Namespace:     "reference",
		Version:       "0.1.0",
		AuthAudiences: []string{"reference-status"},
		AuthScopes:    []string{"reference:status:read"},
	})

	if descriptor.Namespace != "reference" || descriptor.Version != "0.1.0" {
		t.Fatalf("descriptor identity = %+v, want namespace reference version 0.1.0", descriptor)
	}
	if descriptor.SDKVersion != SDKVersion {
		t.Fatalf("descriptor SDK version = %q, want %q", descriptor.SDKVersion, SDKVersion)
	}
	if !reflect.DeepEqual(descriptor.ProtocolVersions, []int{1}) {
		t.Fatalf("descriptor protocol versions = %v, want [1]", descriptor.ProtocolVersions)
	}
}

func TestDescribeCopiesDeclaredAccessSlices(t *testing.T) {
	audiences := []string{"reference-status"}
	descriptor := Describe(Options{Namespace: "reference", Version: "0.1.0", AuthAudiences: audiences})

	audiences[0] = "mutated"

	if descriptor.AuthAudiences[0] != "reference-status" {
		t.Fatal("Describe aliased the caller's audience slice; declared access must not change after description")
	}
}
