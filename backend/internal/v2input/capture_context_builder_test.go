package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
	"time"
)

type captureContextFixture struct {
	seed       CaptureSelectionSeed
	shots      CaptureShotList
	consent    []byte
	frames     []CaptureConfirmedFrame
	consentErr error
}

func (f *captureContextFixture) FreezeSelection(context.Context, CaptureAssessmentLease) (CaptureSelectionSeed, error) {
	return f.seed, nil
}
func (f *captureContextFixture) Load(context.Context, CaptureAssessmentLease, CaptureSelectionSeed) (CaptureShotList, error) {
	return f.shots, nil
}
func (f *captureContextFixture) FreezeConsents(context.Context, CaptureAssessmentLease, CaptureShotList) ([]byte, []uuid.UUID, error) {
	return f.consent, []uuid.UUID{}, f.consentErr
}
func (f *captureContextFixture) LoadHistory(context.Context, CaptureAssessmentLease, CaptureSelectionSeed) ([]CaptureHistoryVersion, error) {
	return []CaptureHistoryVersion{}, nil
}
func (f *captureContextFixture) FreezeCoverage(context.Context, CaptureAssessmentLease, []CaptureHistoryVersion) ([]CaptureConfirmedFrame, error) {
	return f.frames, nil
}

func TestCaptureContextBuilderRechecksConsentAndFrozenSelection(t *testing.T) {
	lease := CaptureAssessmentLease{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), ReceiptID: uuid.New(), SessionID: uuid.New(), AssetVersionID: uuid.New(), Until: time.Now().Add(time.Minute), CreatedAt: time.Now(), Attempts: 1}
	f := &captureContextFixture{seed: CaptureSelectionSeed{1, lease.SessionID, "1", "1", nil, []string{}}, shots: CaptureShotList{1, []CaptureShotRequirement{}}, consent: []byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`), frames: []CaptureConfirmedFrame{}}
	builder := CaptureContextBuilder{f, f}
	evidence, err := builder.Build(context.Background(), lease)
	if err != nil {
		t.Fatal(err)
	}
	shotSHA, _ := canonical.Hash(evidence.Shots)
	consentSHA, _ := canonical.Hash(json.RawMessage(evidence.ConsentBody))
	raw, err := evidence.Compile(lease, Ref{uuid.New(), shotSHA}, Ref{uuid.New(), consentSHA})
	if err != nil || len(raw) == 0 {
		t.Fatal("context compilation failed", err)
	}
	f.consentErr = errors.New("revoked")
	if _, err = builder.Build(context.Background(), lease); !errors.Is(err, f.consentErr) {
		t.Fatal("retry bypassed live consent gate", err)
	}
	f.consentErr = nil
	f.shots.Requirements = []CaptureShotRequirement{{uuid.New(), "Injected", []uuid.UUID{}, 1, .7}}
	if _, err = builder.Build(context.Background(), lease); err == nil {
		t.Fatal("absent frozen list replaced with live requirements")
	}
	f.shots.Requirements = []CaptureShotRequirement{}
	f.frames = []CaptureConfirmedFrame{{uuid.New(), []uuid.UUID{}, nil}}
	if _, err = builder.Build(context.Background(), lease); err == nil {
		t.Fatal("foreign historical version accepted")
	}
}
