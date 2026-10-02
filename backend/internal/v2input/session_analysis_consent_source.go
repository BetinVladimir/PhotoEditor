package v2input

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/canonical"
)

type SQLSessionConsentSource struct {
	DB *pgxpool.Pool
}

// CompileSessionConsentSnapshot validates the relational source's result and
// serializes the exact worker document. Every confirmed face must belong to
// the frozen InputSnapshot; no selected image or identity is inferred.
func CompileSessionConsentSnapshot(raw []byte,
	snapshot SessionInputSnapshot) ([]byte, error) {
	if snapshot.SchemaVersion != 1 || snapshot.SnapshotID == uuid.Nil ||
		len(snapshot.Items) < 1 || len(snapshot.Items) > 5000 ||
		!exactJSONKeys(raw, "schema_version", "grants", "confirmed_faces") {
		return nil, ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || validateSessionConsentSnapshot(canonicalBody) != nil {
		return nil, ErrInvalid
	}
	var doc struct {
		SchemaVersion  int                    `json:"schema_version"`
		Grants         []sessionConsentGrant  `json:"grants"`
		ConfirmedFaces []sessionConfirmedFace `json:"confirmed_faces"`
	}
	decoder := json.NewDecoder(bytes.NewReader(canonicalBody))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&doc) != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, ErrInvalid
	}
	selected := make(map[uuid.UUID]bool, len(snapshot.Items))
	for _, item := range snapshot.Items {
		selected[item.Binding.AssetVersionID] = true
	}
	for _, face := range doc.ConfirmedFaces {
		if !selected[face.AssetVersionID] {
			return nil, ErrInvalid
		}
	}
	return canonicalBody, nil
}

func (s SQLSessionConsentSource) Load(ctx context.Context, tenant, run uuid.UUID,
	snapshot SessionInputSnapshot) ([]byte, error) {
	if s.DB == nil || tenant == uuid.Nil || run == uuid.Nil {
		return nil, ErrInvalid
	}
	expected, err := AnalysisSnapshotID(run)
	if err != nil || snapshot.SnapshotID != expected {
		return nil, ErrInvalid
	}
	var raw []byte
	if err := s.DB.QueryRow(ctx, `SELECT photo.v22_analysis_consent_sources($1,$2)`,
		tenant, run).Scan(&raw); err != nil {
		return nil, err
	}
	return CompileSessionConsentSnapshot(raw, snapshot)
}
