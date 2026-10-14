package semanticreview

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func TestTargetResourceOwnsPinsAndPreservesRevisionPrecision(t *testing.T) {
	candidate, child := uuid.New(), uuid.New()
	row := TargetRow{ID: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), IntentID: uuid.New(), AssetVersionID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now(), Revision: 9007199254740993, State: "applied", CandidateArtifactID: &candidate, ChildRunID: &child}
	document := TargetDocument{candidate, strings.Repeat("a", 64)}
	result, err := NewTargetResource(row, &document)
	if err != nil {
		t.Fatal(err)
	}
	oldCandidate, oldChild := candidate, child
	candidate = uuid.New()
	child = uuid.New()
	document.SHA256 = strings.Repeat("b", 64)
	if *result.Data.CandidateArtifactID != oldCandidate || *result.Data.ChildRunID != oldChild || result.Document.SHA256 != strings.Repeat("a", 64) {
		t.Fatal("mutable output authority")
	}
	body, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]any
	if json.Unmarshal(body, &wire) != nil || wire["revision"] != "9007199254740993" || wire["data"].(map[string]any)["revision"] != "9007199254740993" {
		t.Fatal("revision precision lost")
	}
	if wire["data"].(map[string]any)["state"] != "applied" {
		t.Fatal("target completion hidden")
	}
}

func TestTargetResourceRejectsSubstitutedDocumentAndUnknownState(t *testing.T) {
	candidate := uuid.New()
	row := TargetRow{ID: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), IntentID: uuid.New(), AssetVersionID: uuid.New(), CreatedAt: time.Now(), UpdatedAt: time.Now(), Revision: 1, State: "applied", CandidateArtifactID: &candidate}
	if _, err := NewTargetResource(row, &TargetDocument{uuid.New(), strings.Repeat("a", 64)}); err == nil {
		t.Fatal("foreign candidate accepted")
	}
	row.State = "approved"
	if _, err := NewTargetResource(row, nil); err == nil {
		t.Fatal("unpersisted state accepted")
	}
}
