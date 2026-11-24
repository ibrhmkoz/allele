package main

import "runtime/debug"

// version is set by the release build (-ldflags "-X main.version=1.2.3").
var version string

// versionOf is what allele -version prints: the release build's version,
// else the module version go install records, else "devel".
func versionOf(release string, info *debug.BuildInfo, ok bool) string {
	switch {
	case release != "":
		return release
	case ok && info.Main.Version != "" && info.Main.Version != "(devel)":
		return info.Main.Version
	}
	return "devel"
}
