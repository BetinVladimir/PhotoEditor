package v2input

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"

	"photoeditor/backend/internal/canonical"
)

type CaptureContextStore interface {
	FreezeSelection(context.Context, CaptureAssessmentLease) (CaptureSelectionSeed, error)
	FreezeConsents(context.Context, CaptureAssessmentLease, CaptureShotList) ([]byte, []uuid.UUID, error)
	LoadHistory(context.Context, CaptureAssessmentLease, CaptureSelectionSeed) ([]CaptureHistoryVersion, error)
	FreezeCoverage(context.Context, CaptureAssessmentLease, []CaptureHistoryVersion) ([]CaptureConfirmedFrame, error)
}
type CaptureContextShots interface {
	Load(context.Context, CaptureAssessmentLease, CaptureSelectionSeed) (CaptureShotList, error)
}
type CaptureContextBuilder struct {
	Store CaptureContextStore
	Shots CaptureContextShots
}
type FrozenCaptureContextEvidence struct {
	Selection   CaptureSelectionSeed
	Shots       CaptureShotList
	ShotBody    []byte
	ConsentBody []byte
	History     []CaptureHistoryVersion
	Frames      []CaptureConfirmedFrame
}

// Build freezes all relational sources before object publication. Every retry
// obtains the original seeds and repeats live consent gates, not live history.
func (b CaptureContextBuilder) Build(ctx context.Context, lease CaptureAssessmentLease) (FrozenCaptureContextEvidence, error) {
	var result FrozenCaptureContextEvidence
	if b.Store == nil || b.Shots == nil || !validCaptureLease(lease) {
		return result, ErrInvalid
	}
	selection, err := b.Store.FreezeSelection(ctx, lease)
	if err != nil {
		return result, err
	}
	raw, err := json.Marshal(selection)
	if err != nil {
		return result, ErrInvalid
	}
	if _, err = CompileCaptureSelection(raw, lease.SessionID); err != nil {
		return result, err
	}
	shots, err := b.Shots.Load(ctx, lease, selection)
	if err != nil {
		return result, err
	}
	shotBody, err := CompileCaptureShotList(shots)
	if err != nil {
		return result, err
	}
	shotSHA, err := canonical.Hash(shots)
	if err != nil || selection.ShotDocument != nil && shotSHA != selection.ShotDocument.SHA256 || selection.ShotDocument == nil && len(shots.Requirements) != 0 {
		return result, ErrInvalid
	}
	consents, _, err := b.Store.FreezeConsents(ctx, lease, shots)
	if err != nil {
		return result, err
	}
	consentBody, _, err := CompileCaptureConsentSnapshot(consents, lease.AssetVersionID, shots)
	if err != nil {
		return result, err
	}
	history, err := b.Store.LoadHistory(ctx, lease, selection)
	if err != nil {
		return result, err
	}
	raw, err = json.Marshal(history)
	if err != nil {
		return result, ErrInvalid
	}
	if _, err = CompileCaptureHistory(raw, selection); err != nil {
		return result, err
	}
	frames, err := b.Store.FreezeCoverage(ctx, lease, history)
	if err != nil {
		return result, err
	}
	allowed := map[uuid.UUID]bool{}
	for _, row := range history {
		allowed[row.AssetVersionID] = true
	}
	for _, frame := range frames {
		if frame.AssetVersionID == lease.AssetVersionID || !allowed[frame.AssetVersionID] {
			return result, ErrInvalid
		}
	}
	if _, err = CompileCaptureCoverage(shots, frames); err != nil {
		return result, err
	}
	return FrozenCaptureContextEvidence{selection, shots, shotBody, consentBody, history, frames}, nil
}

func (e FrozenCaptureContextEvidence) Compile(lease CaptureAssessmentLease, shots, consents Ref) ([]byte, error) {
	if !validCaptureLease(lease) || e.Selection.SessionID != lease.SessionID {
		return nil, ErrInvalid
	}
	raw, err := json.Marshal(e.History)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = CompileCaptureHistory(raw, e.Selection); err != nil {
		return nil, err
	}
	allowed := map[uuid.UUID]bool{}
	for _, row := range e.History {
		allowed[row.AssetVersionID] = true
	}
	for _, frame := range e.Frames {
		if !allowed[frame.AssetVersionID] {
			return nil, ErrInvalid
		}
	}
	return CompileCaptureContextEvidence(e.Selection, lease.AssetVersionID, e.Shots, shots, e.ConsentBody, consents, e.Frames)
}
