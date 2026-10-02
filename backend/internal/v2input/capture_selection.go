package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// CaptureSelectionSeed fixes the session revision and document selection before
// mutable context is resolved. Consent and qualified coverage are separate gates.
type CaptureSelectionSeed struct {
	SchemaVersion     int                  `json:"schema_version"`
	SessionID         uuid.UUID            `json:"session_id"`
	SessionRevision   string               `json:"session_revision"`
	ConsentEpoch      string               `json:"consent_epoch"`
	ShotDocument      *CaptureShotDocument `json:"shot_document"`
	RecentContentSHAs []string             `json:"recent_content_shas"`
}
type CaptureShotDocument struct {
	DocumentID uuid.UUID `json:"document_id"`
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
}

func CompileCaptureSelection(raw []byte, session uuid.UUID) (CaptureSelectionSeed, error) {
	var seed CaptureSelectionSeed
	if session == uuid.Nil || len(raw) < 2 || len(raw) > 1048576 || !exactJSONKeys(raw, "schema_version", "session_id", "session_revision", "consent_epoch", "shot_document", "recent_content_shas") {
		return seed, ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return seed, ErrInvalid
	}
	if !bytes.Equal(bytes.TrimSpace(fields["shot_document"]), []byte("null")) && !exactJSONKeys(fields["shot_document"], "document_id", "artifact_id", "sha256") {
		return seed, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&seed) != nil || decoder.Decode(new(any)) != io.EOF {
		return seed, ErrInvalid
	}
	if seed.SchemaVersion != 1 || seed.SessionID != session || seed.RecentContentSHAs == nil || len(seed.RecentContentSHAs) > 5000 {
		return seed, ErrInvalid
	}
	for _, value := range []string{seed.SessionRevision, seed.ConsentEpoch} {
		if len(value) == 0 || value[0] == '0' {
			return seed, ErrInvalid
		}
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil || n < 1 || strconv.FormatInt(n, 10) != value {
			return seed, ErrInvalid
		}
	}
	if seed.ShotDocument != nil && (seed.ShotDocument.DocumentID == uuid.Nil || !validRef(Ref{seed.ShotDocument.ArtifactID, seed.ShotDocument.SHA256})) {
		return seed, ErrInvalid
	}
	for _, sha := range seed.RecentContentSHAs {
		if !digestPattern.MatchString(sha) {
			return seed, ErrInvalid
		}
	}
	if _, err := canonical.Marshal(seed); err != nil {
		return seed, ErrInvalid
	}
	return seed, nil
}

func (s SQLCaptureAssessmentStore) FreezeSelection(ctx context.Context, lease CaptureAssessmentLease) (CaptureSelectionSeed, error) {
	if s.DB == nil || !validCaptureLease(lease) {
		return CaptureSelectionSeed{}, ErrInvalid
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.freeze_capture_selection($1,$2,$3)`, lease.TenantID, lease.JobID, lease.Token).Scan(&raw); err != nil {
		return CaptureSelectionSeed{}, err
	}
	if len(raw) == 0 {
		return CaptureSelectionSeed{}, ErrExpired
	}
	return CompileCaptureSelection(raw, lease.SessionID)
}
