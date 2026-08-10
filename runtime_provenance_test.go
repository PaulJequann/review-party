package reviewparty

import (
	"reflect"
	"runtime/debug"
	"testing"
)

func TestRuntimeProvenanceUsesOnlyEstablishedBuildInformation(t *testing.T) {
	information := &debug.BuildInfo{
		Main: debug.Module{Version: "v0.4.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "abc123"},
			{Key: "vcs.modified", Value: "false"},
		},
	}
	modified := false
	want := RuntimeProvenance{Version: "v0.4.0", VCSRevision: "abc123", VCSModified: &modified}
	if got := runtimeProvenanceFrom(information, true); !reflect.DeepEqual(got, want) {
		t.Fatalf("provenance = %#v, want %#v", got, want)
	}

	development := &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}}
	if got := runtimeProvenanceFrom(development, true); !reflect.DeepEqual(got, RuntimeProvenance{}) {
		t.Fatalf("development provenance = %#v", got)
	}
	if got := runtimeProvenanceFrom(nil, false); !reflect.DeepEqual(got, RuntimeProvenance{}) {
		t.Fatalf("missing provenance = %#v", got)
	}
}
