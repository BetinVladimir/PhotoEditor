package main

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestImportArgsRejectBypassFlagsAndMissingIdentity(t *testing.T) {
	id := uuid.New().String()
	args := []string{"import", "--tenant", id, "--project", id,
		"--operator", id, "--report-artifact", id,
		"--sha256", strings.Repeat("a", 64), "--locks", "/trusted/locks.json"}
	if _, err := parseImportArgs(args); err != nil {
		t.Fatal(err)
	}
	if _, err := parseImportArgs(append(append([]string{}, args...), "--skip-tests")); err == nil {
		t.Fatal("bypass flag accepted")
	}
	bad := append([]string{}, args...)
	bad[2] = uuid.Nil.String()
	if _, err := parseImportArgs(bad); err == nil {
		t.Fatal("nil tenant accepted")
	}
	if _, err := parseImportArgs([]string{"import"}); err == nil {
		t.Fatal("missing inputs accepted")
	}
}
