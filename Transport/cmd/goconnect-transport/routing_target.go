package main

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

func appBundleRoutingPath(path string) bool {
	return strings.EqualFold(filepath.Ext(path), ".app")
}

func validRoutingTargetPath(path string, allowExecutable bool) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, ",\x00\r\n") {
		return false
	}
	return appBundleRoutingPath(path) || allowExecutable
}

func processPathPattern(path string) string {
	pattern := "^" + regexp.QuoteMeta(path)
	if appBundleRoutingPath(path) {
		return pattern + "/.*"
	}
	return pattern + "$"
}

func appBundleContentsPathPattern(path string) string {
	return "^" + regexp.QuoteMeta(path) + "/Contents/.*"
}

func processPathRule(path, target string) string {
	return "PROCESS-PATH-REGEX," + processPathPattern(path) + "," + target
}

func appChildRule(path, target string, uid uint32) string {
	return "GOCONNECT-APP-CHILD," + path + "," + target + "," + strconv.FormatUint(uint64(uid), 10)
}

func configuredPathMatches(path, processPath string) bool {
	if appBundleRoutingPath(path) {
		return strings.HasPrefix(processPath, path+"/")
	}
	return processPath == path
}
