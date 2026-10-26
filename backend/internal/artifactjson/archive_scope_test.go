package artifactjson

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/archivecontract"
	"testing"
)

func TestArchiveParams8MiBScopeIsExplicitAndClosed(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "task_params", DownloadArchive: true}
	if maxScopeDocumentBytes(s) != archivecontract.DocumentBytes {
		t.Fatal("archivebound")
	}
	if _, e := DocumentKey(s, uuid.New()); e != nil {
		t.Fatal(e)
	}
	normal := s
	normal.DownloadArchive = false
	if maxScopeDocumentBytes(normal) != 1048576 {
		t.Fatal("ordinaryparamsbroadened")
	}
	for _, change := range []func(*Scope){func(s *Scope) { s.Purpose = "review_selection" }, func(s *Scope) { s.LocalPortrait = true }, func(s *Scope) { s.AssemblyLease = uuid.New() }} {
		bad := s
		change(&bad)
		if _, e := DocumentKey(bad, uuid.New()); e == nil {
			t.Fatal("wrongscopeadmitted")
		}
	}
}
