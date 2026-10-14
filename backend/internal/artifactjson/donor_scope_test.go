package artifactjson

import (
	"github.com/google/uuid"
	"testing"
)

func TestDonorParamsScopeRejectsMixedAuthorities(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", DonorPortrait: true}
	if _, err := DocumentKey(s, uuid.New()); err != nil {
		t.Fatal(err)
	}
	if maxScopeDocumentBytes(s) != 65536 {
		t.Fatal("donor limit")
	}
	for _, change := range []func(*Scope){
		func(s *Scope) { s.LocalPortrait = true },
		func(s *Scope) { s.AssemblyLease = uuid.New() },
		func(s *Scope) { s.Purpose = "v2_input_manifest" },
		func(s *Scope) { s.Purpose = "review_selection" },
	} {
		bad := s
		change(&bad)
		if _, err := DocumentKey(bad, uuid.New()); err == nil {
			t.Fatal("mixed/unrelated scope accepted")
		}
	}
}
