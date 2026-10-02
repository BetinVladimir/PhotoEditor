package v2input

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
)

func TestCaptureConsentsDoNotTurnRequirementsIntoConfirmedPeople(t *testing.T) {
	person, version, grant := uuid.New(), uuid.New(), uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{{uuid.New(), "Portrait", []uuid.UUID{person}, 1, .7}}}
	doc := struct {
		SchemaVersion  int                    `json:"schema_version"`
		Grants         []sessionConsentGrant  `json:"grants"`
		ConfirmedFaces []sessionConfirmedFace `json:"confirmed_faces"`
	}{1, []sessionConsentGrant{{grant, "2", &person, "identity_matching"}}, []sessionConfirmedFace{}}
	raw, _ := json.Marshal(doc)
	if body, people, err := CompileCaptureConsentSnapshot(raw, version, shots); err != nil || len(body) == 0 || len(people) != 0 {
		t.Fatalf("required person was confirmed: %v %v", people, err)
	}
	doc.ConfirmedFaces = []sessionConfirmedFace{{version, uuid.New(), person, grant, "2"}}
	raw, _ = json.Marshal(doc)
	if _, people, err := CompileCaptureConsentSnapshot(raw, version, shots); err != nil || len(people) != 1 || people[0] != person {
		t.Fatalf("confirmation missing: %v %v", people, err)
	}
	doc.ConfirmedFaces[0].AssetVersionID = uuid.New()
	raw, _ = json.Marshal(doc)
	if _, _, err := CompileCaptureConsentSnapshot(raw, version, shots); err == nil {
		t.Fatal("foreign receipt face accepted")
	}
	doc.ConfirmedFaces = []sessionConfirmedFace{}
	doc.Grants = []sessionConsentGrant{}
	raw, _ = json.Marshal(doc)
	if _, _, err := CompileCaptureConsentSnapshot(raw, version, shots); err == nil {
		t.Fatal("missing requirement consent accepted")
	}
	doc.Grants = []sessionConsentGrant{{grant, "2", &person, "learning_person"}}
	raw, _ = json.Marshal(doc)
	if _, _, err := CompileCaptureConsentSnapshot(raw, version, shots); err == nil {
		t.Fatal("learning consent accepted for identity")
	}
	empty := CaptureShotList{1, []CaptureShotRequirement{}}
	doc.Grants = []sessionConsentGrant{{grant, "2", &person, "identity_matching"}}
	raw, _ = json.Marshal(doc)
	if _, _, err := CompileCaptureConsentSnapshot(raw, version, empty); err == nil {
		t.Fatal("unrelated identity grant included")
	}
}

func TestCaptureConsentsRejectsAmbiguousNestedEpoch(t *testing.T) {
	person, version, grant := uuid.New(), uuid.New(), uuid.New()
	shots := CaptureShotList{1, []CaptureShotRequirement{{uuid.New(), "Portrait", []uuid.UUID{person}, 1, .7}}}
	doc := map[string]any{"schema_version": 1, "grants": []sessionConsentGrant{{grant, "2", &person, "identity_matching"}}, "confirmed_faces": []sessionConfirmedFace{}}
	raw, _ := json.Marshal(doc)
	bad := strings.Replace(string(raw), `"epoch":"2"`, `"epoch":"1","epoch":"2"`, 1)
	if _, _, err := CompileCaptureConsentSnapshot([]byte(bad), version, shots); err == nil {
		t.Fatal("duplicate consent epoch accepted")
	}
}
