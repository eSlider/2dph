package main

import "fmt"

// brainWriteFunc is the injection point for the --write-brain mode. The
// concrete implementation lives in brainwrite_impl.go behind the
// //go:build cgo && system_ladybug tag. Without those tags it is a stub that
// tells the user how to enable brain writing.
var brainWriteFunc = func(cs []Contact, dbPath string) error {
	return fmt.Errorf("--write-brain requires build tags cgo + system_ladybug; " +
		"build with: go build -tags=system_ladybug ./bin/contacts")
}
