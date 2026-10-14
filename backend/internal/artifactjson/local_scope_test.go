package artifactjson

import (
	"github.com/google/uuid"
	"testing"
)

func TestLocalPortraitScopeIsLimitedToUnfencedParams(t *testing.T) {
	scope := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", LocalPortrait: true}
	if _, err := DocumentKey(scope, uuid.New()); err != nil || maxScopeDocumentBytes(scope) != 65536 {
		t.Fatal("valid local scope rejected", err)
	}
	scope.AssemblyLease = uuid.New()
	if _, err := DocumentKey(scope, uuid.New()); err == nil {
		t.Fatal("mixed authority accepted")
	}
	scope.AssemblyLease = uuid.Nil
	scope.Purpose = "v2_input_manifest"
	if _, err := DocumentKey(scope, uuid.New()); err == nil {
		t.Fatal("transport routed through params authority")
	}
}
