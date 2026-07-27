package protocol

import (
	"reflect"
	"testing"
)

func TestParseVersionsSortsNewestFirstAndDropsInvalidEntries(t *testing.T) {
	tests := []struct {
		name string
		list string
		want []int
	}{
		{name: "single", list: "1", want: []int{1}},
		{name: "sorted newest first", list: "1,3,2", want: []int{3, 2, 1}},
		{name: "duplicates removed", list: "2,2,1", want: []int{2, 1}},
		{name: "whitespace tolerated", list: " 2 , 1 ", want: []int{2, 1}},
		{name: "non numeric dropped", list: "1,v2,x", want: []int{1}},
		{name: "non positive dropped", list: "0,-1,1", want: []int{1}},
		{name: "empty", list: "", want: nil},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ParseVersions(test.list); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("ParseVersions(%q) = %v, want %v", test.list, got, test.want)
			}
		})
	}
}

func TestSupportedReflectsBuildTimeVersion(t *testing.T) {
	if got := Supported(); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("Supported() = %v, want [1]; default protocol version changed without updating the shell", got)
	}
}
