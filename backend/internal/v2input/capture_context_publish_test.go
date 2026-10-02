package v2input

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"testing"
	"time"
)

type captureDocumentStageFake struct {
	bodies map[string][]byte
	fail   bool
}

func (s *captureDocumentStageFake) StageDocumentBody(_ context.Context, _ CaptureAssessmentLease, purpose string, body []byte) error {
	if s.fail {
		return ErrExpired
	}
	if old, ok := s.bodies[purpose]; ok && !bytes.Equal(old, body) {
		return ErrConflict
	}
	s.bodies[purpose] = append([]byte(nil), body...)
	return nil
}

func TestCaptureContextPublicationResumesExactDependencies(t *testing.T) {
	lease := CaptureAssessmentLease{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), ReceiptID: uuid.New(), SessionID: uuid.New(), AssetVersionID: uuid.New(), Until: time.Now().Add(time.Minute), CreatedAt: time.Now(), Attempts: 1}
	evidence := FrozenCaptureContextEvidence{Selection: CaptureSelectionSeed{1, lease.SessionID, "1", "1", nil, []string{}}, Shots: CaptureShotList{1, []CaptureShotRequirement{}}, ShotBody: []byte(`{"requirements":[],"schema_version":1}`), ConsentBody: []byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`), History: []CaptureHistoryVersion{}, Frames: []CaptureConfirmedFrame{}}
	stage := &captureDocumentStageFake{bodies: map[string][]byte{}}
	docs := &stableAnalysisDocuments{refs: map[string]artifactjson.Ref{}}
	first, err := PublishCaptureContext(context.Background(), stage, docs, lease, evidence)
	if err != nil || len(docs.scopes) != 3 {
		t.Fatalf("first=%v err=%v scopes=%v", first, err, docs.scopes)
	}
	second, err := PublishCaptureContext(context.Background(), stage, docs, lease, evidence)
	if err != nil || first.Context != second.Context || first.Shots != second.Shots || first.Consents != second.Consents || !bytes.Equal(first.Body, second.Body) {
		t.Fatalf("retry changed context: %v %v", second, err)
	}
	evidence.Selection.ShotDocument = &CaptureShotDocument{uuid.New(), first.Shots.ArtifactID, first.Shots.SHA256}
	previous := len(docs.scopes)
	if _, err = PublishCaptureContext(context.Background(), stage, docs, lease, evidence); err != nil || len(docs.scopes) != previous+2 {
		t.Fatal("existing frozen shot list republished", err)
	}
	stage.fail = true
	previous = len(docs.scopes)
	if _, err = PublishCaptureContext(context.Background(), stage, docs, lease, evidence); err != ErrExpired || len(docs.scopes) != previous {
		t.Fatal("lost lease wrote an object", err)
	}
	stage.fail = false
	evidence.ShotBody = []byte(`{}`)
	if _, err = PublishCaptureContext(context.Background(), stage, docs, lease, evidence); err == nil || len(docs.scopes) != previous {
		t.Fatal("mismatched frozen body published")
	}
}
