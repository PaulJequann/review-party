package reviewparty

import "runtime/debug"

func currentRuntimeProvenance() RuntimeProvenance {
	return runtimeProvenanceFrom(debug.ReadBuildInfo())
}

func runtimeProvenanceFrom(information *debug.BuildInfo, available bool) RuntimeProvenance {
	if !available || information == nil {
		return RuntimeProvenance{}
	}
	provenance := RuntimeProvenance{}
	if information.Main.Version != "" && information.Main.Version != "(devel)" {
		provenance.Version = information.Main.Version
	}
	for _, setting := range information.Settings {
		applyBuildSetting(&provenance, setting)
	}
	return provenance
}

func applyBuildSetting(provenance *RuntimeProvenance, setting debug.BuildSetting) {
	switch setting.Key {
	case "vcs.revision":
		provenance.VCSRevision = setting.Value
	case "vcs.modified":
		modified, known := parseBuildModified(setting.Value)
		if known {
			provenance.VCSModified = &modified
		}
	}
}

func parseBuildModified(value string) (bool, bool) {
	switch value {
	case "true":
		return true, true
	case "false":
		return false, true
	default:
		return false, false
	}
}
