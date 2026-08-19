//usr/bin/env go run "$0" "$@"; exit
//
// bin/ci/semver.go - next semver from conventional commits since the last tag.
//
//	bin/ci/semver.go                      # last tag..HEAD
//	bin/ci/semver v0.1.0 v0.1.0..HEAD  # explicit tag + range
//	prints: v0.1.1 | v0.2.0 | v1.0.0 | none
//
// Bump rules (conventional commits):
//
//	BREAKING CHANGE / feat!   -> major
//	feat:                      -> minor
//	fix:, perf:, refactor:,... -> patch
//	no commits in range        -> none
//
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func git(args ...string) []string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		return nil
	}
	var res []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if s := strings.TrimSpace(l); s != "" {
			res = append(res, s)
		}
	}
	return res
}

func lastTag() string {
	for _, t := range git("tag", "--sort=-v:refname") {
		if strings.HasPrefix(t, "v") {
			return t
		}
	}
	return ""
}

func subjectsFor(range_ string) []string {
	args := []string{"log", "--format=%s"}
	if range_ != "" && range_ != "HEAD" {
		args = append(args, range_)
	}
	return git(args...)
}

func bumpType(subjects []string) string {
	if len(subjects) == 0 {
		return "none"
	}
	for _, s := range subjects {
		text := strings.ToLower(s)
		if strings.Contains(text, "breaking change") || strings.Contains(text, "breaking-change") {
			return "major"
		}
		prefix := s
		if i := strings.Index(s, ":"); i >= 0 {
			prefix = s[:i]
		}
		if strings.Contains(prefix, "!") {
			return "major"
		}
	}
	for _, s := range subjects {
		if strings.HasPrefix(s, "feat:") {
			return "minor"
		}
	}
	return "patch"
}

func bumpVersion(current, bump string) *string {
	if bump == "none" {
		return nil
	}
	v := current
	if v == "" {
		v = "0.0.0"
	}
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	at := func(i int) int {
		if i >= len(parts) {
			return 0
		}
		n, _ := strconv.Atoi(parts[i])
		return n
	}
	major, minor, patch := at(0), at(1), at(2)
	var out string
	switch bump {
	case "major":
		out = fmt.Sprintf("v%d.0.0", major+1)
	case "minor":
		out = fmt.Sprintf("v%d.%d.0", major, minor+1)
	default:
		out = fmt.Sprintf("v%d.%d.%d", major, minor, patch+1)
	}
	return &out
}

func main() {
	args := os.Args[1:]
	tag := ""
	if len(args) > 1 {
		tag = args[0]
	} else if len(args) == 1 {
		tag = args[0]
	} else {
		tag = lastTag()
	}
	range_ := "HEAD"
	if len(args) >= 2 {
		range_ = args[1]
	} else if tag != "" {
		range_ = tag + "..HEAD"
	}
	if next := bumpVersion(tag, bumpType(subjectsFor(range_))); next != nil {
		fmt.Println(*next)
	} else {
		fmt.Println("none")
	}
}