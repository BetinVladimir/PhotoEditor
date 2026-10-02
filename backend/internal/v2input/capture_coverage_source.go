package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"

	"github.com/google/uuid"
)

type CaptureCoverageIdentity struct {
	PersonID  uuid.UUID `json:"person_id"`
	ConsentID uuid.UUID `json:"consent_id"`
	Epoch     string    `json:"epoch"`
}
type CaptureCoverageEvidence struct {
	ConfirmationID   uuid.UUID                 `json:"confirmation_id"`
	ReceiptID        uuid.UUID                 `json:"receipt_id"`
	AssetVersionID   uuid.UUID                 `json:"asset_version_id"`
	Quality          *float64                  `json:"quality"`
	Report           Ref                       `json:"report"`
	IdentityEvidence []CaptureCoverageIdentity `json:"identity_evidence"`
}

// CompileCaptureCoverageEvidence verifies exact frozen receipt/version scope.
// SQL proves report adoption and live grant epochs; this compiler proves the
// closed packet and prevents current-frame or automatic-match coverage.
func CompileCaptureCoverageEvidence(raw []byte, current uuid.UUID, history []CaptureHistoryVersion) ([]CaptureConfirmedFrame, error) {
	if current == uuid.Nil || history == nil || len(history) > 5000 || len(raw) < 2 || len(raw) > 8388608 {
		return nil, ErrInvalid
	}
	allowed := map[uuid.UUID]uuid.UUID{}
	for _, row := range history {
		if row.ReceiptID == uuid.Nil || row.AssetVersionID == uuid.Nil || !digestPattern.MatchString(row.ContentSHA) || allowed[row.ReceiptID] != uuid.Nil {
			return nil, ErrInvalid
		}
		allowed[row.ReceiptID] = row.AssetVersionID
	}
	var rows []CaptureCoverageEvidence
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&rows) != nil || decoder.Decode(new(any)) != io.EOF || rows == nil || len(rows) > len(history) {
		return nil, ErrInvalid
	}
	var packets []json.RawMessage
	if json.Unmarshal(raw, &packets) != nil {
		return nil, ErrInvalid
	}
	for _, packet := range packets {
		if !exactJSONKeys(packet, "confirmation_id", "receipt_id", "asset_version_id", "quality", "report", "identity_evidence") {
			return nil, ErrInvalid
		}

		var fields map[string]json.RawMessage
		if json.Unmarshal(packet, &fields) != nil || !exactJSONKeys(fields["report"], "artifact_id", "sha256") {
			return nil, ErrInvalid
		}
		var identities []json.RawMessage
		if json.Unmarshal(fields["identity_evidence"], &identities) != nil || identities == nil {
			return nil, ErrInvalid
		}
		for _, identity := range identities {
			if !exactJSONKeys(identity, "person_id", "consent_id", "epoch") {
				return nil, ErrInvalid
			}
		}
	}
	confirmations := map[uuid.UUID]bool{}
	seenReceipts := map[uuid.UUID]bool{}
	frames := make([]CaptureConfirmedFrame, 0, len(rows))
	for _, row := range rows {
		if row.ConfirmationID == uuid.Nil || confirmations[row.ConfirmationID] || seenReceipts[row.ReceiptID] || row.AssetVersionID == current || row.AssetVersionID == uuid.Nil || allowed[row.ReceiptID] != row.AssetVersionID || !validRef(row.Report) || row.IdentityEvidence == nil || len(row.IdentityEvidence) > 100 {
			return nil, ErrInvalid
		}
		if row.Quality != nil && (math.IsNaN(*row.Quality) || math.IsInf(*row.Quality, 0) || *row.Quality < 0 || *row.Quality > 1) {
			return nil, ErrInvalid
		}
		confirmations[row.ConfirmationID] = true
		seenReceipts[row.ReceiptID] = true
		people := make([]uuid.UUID, 0, len(row.IdentityEvidence))
		seen := map[uuid.UUID]bool{}
		seenConsents := map[uuid.UUID]bool{}
		for _, identity := range row.IdentityEvidence {
			if identity.PersonID == uuid.Nil || identity.ConsentID == uuid.Nil || !validConsentEpoch(identity.Epoch) || seen[identity.PersonID] || seenConsents[identity.ConsentID] {
				return nil, ErrInvalid
			}
			seen[identity.PersonID] = true
			seenConsents[identity.ConsentID] = true
			people = append(people, identity.PersonID)
		}
		frames = append(frames, CaptureConfirmedFrame{row.AssetVersionID, people, row.Quality})
	}
	return frames, nil
}

func (s SQLCaptureAssessmentStore) FreezeCoverage(ctx context.Context, lease CaptureAssessmentLease, history []CaptureHistoryVersion) ([]CaptureConfirmedFrame, error) {
	if s.DB == nil || !validCaptureLease(lease) {
		return nil, ErrInvalid
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.freeze_capture_coverage($1,$2,$3)`, lease.TenantID, lease.JobID, lease.Token).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrExpired
	}
	return CompileCaptureCoverageEvidence(raw, lease.AssetVersionID, history)
}
