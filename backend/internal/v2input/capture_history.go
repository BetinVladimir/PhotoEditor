package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
)

// CaptureHistoryVersion identifies the immutable receipt/version pair selected
// alongside the recent original-content digests. Quality must bind this version.
type CaptureHistoryVersion struct {
	ReceiptID      uuid.UUID `json:"receipt_id"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	ContentSHA     string    `json:"content_sha"`
}

func CompileCaptureHistory(raw []byte, seed CaptureSelectionSeed) ([]CaptureHistoryVersion, error) {
	if len(raw) < 2 || len(raw) > 2097152 {
		return nil, ErrInvalid
	}
	selection, err := json.Marshal(seed)
	if err != nil {
		return nil, ErrInvalid
	}
	if _, err = CompileCaptureSelection(selection, seed.SessionID); err != nil {
		return nil, err
	}
	var history []CaptureHistoryVersion
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&history) != nil || decoder.Decode(new(any)) != io.EOF || history == nil || len(history) > 5000 || len(history) != len(seed.RecentContentSHAs) {
		return nil, ErrInvalid
	}
	var packets []json.RawMessage
	if json.Unmarshal(raw, &packets) != nil {
		return nil, ErrInvalid
	}
	for _, packet := range packets {
		if !exactJSONKeys(packet, "receipt_id", "asset_version_id", "content_sha") {
			return nil, ErrInvalid
		}
	}

	receipts := map[uuid.UUID]bool{}
	for i, row := range history {
		if row.ReceiptID == uuid.Nil || row.AssetVersionID == uuid.Nil || receipts[row.ReceiptID] || !digestPattern.MatchString(row.ContentSHA) || row.ContentSHA != seed.RecentContentSHAs[i] {
			return nil, ErrInvalid
		}
		receipts[row.ReceiptID] = true
	}
	return history, nil
}

func (s SQLCaptureAssessmentStore) LoadHistory(ctx context.Context, lease CaptureAssessmentLease, seed CaptureSelectionSeed) ([]CaptureHistoryVersion, error) {
	if s.DB == nil || !validCaptureLease(lease) || seed.SessionID != lease.SessionID {
		return nil, ErrInvalid
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.capture_history_source($1,$2,$3)`, lease.TenantID, lease.JobID, lease.Token).Scan(&raw); err != nil {
		return nil, err
	}
	if len(raw) == 0 {
		return nil, ErrExpired
	}
	return CompileCaptureHistory(raw, seed)
}
