package main

import "runtime/debug"

// version is set by the release build (-ldflags "-X main.version=1.2.3").
var version string

func versionOf(release string, info *debug.BuildInfo, ok bool) string {
	switch {
	case release != "":
		return release
	case ok && info.Main.Version != "" && info.Main.Version != "(devel)":
		return info.Main.Version
	}
	return "devel"
}
