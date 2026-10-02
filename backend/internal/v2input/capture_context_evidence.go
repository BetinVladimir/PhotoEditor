package v2input

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// CompileCaptureContextEvidence joins frozen selection with exact published
// dependencies. The frame source must independently prove session membership,
// confirmation and qualified quality provenance for every historical version.
func CompileCaptureContextEvidence(seed CaptureSelectionSeed, currentVersion uuid.UUID,
	shots CaptureShotList, shotsRef Ref, consentBody []byte, consentsRef Ref,
	frames []CaptureConfirmedFrame) ([]byte, error) {
	raw, err := json.Marshal(seed)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = CompileCaptureSelection(raw, seed.SessionID); err != nil {
		return nil, err
	}
	if !validRef(shotsRef) || !validRef(consentsRef) || currentVersion == uuid.Nil {
		return nil, ErrInvalid
	}
	if seed.ShotDocument != nil {
		if shotsRef != (Ref{seed.ShotDocument.ArtifactID, seed.ShotDocument.SHA256}) {
			return nil, ErrInvalid
		}
	} else if len(shots.Requirements) != 0 {
		return nil, ErrInvalid
	}
	canonicalConsents, confirmed, err := CompileCaptureConsentSnapshot(consentBody, currentVersion, shots)
	if err != nil {
		return nil, err
	}
	digest, err := canonical.Hash(json.RawMessage(canonicalConsents))
	if err != nil || digest != consentsRef.SHA256 {
		return nil, ErrInvalid
	}
	if frames == nil || len(frames) > len(seed.RecentContentSHAs) {
		return nil, ErrInvalid
	}
	for _, frame := range frames {
		if frame.AssetVersionID == currentVersion {
			return nil, ErrInvalid
		}
	}
	coverage, err := CompileCaptureCoverage(shots, frames)
	if err != nil {
		return nil, err
	}
	context := CaptureContext{SchemaVersion: 1, SessionID: seed.SessionID, ConfirmedPersonIDs: confirmed,
		ShotList: shotsRef, RecentContentSHAs: seed.RecentContentSHAs, Consents: consentsRef, Coverage: coverage}
	return CompileCaptureContext(context, shots)
}
