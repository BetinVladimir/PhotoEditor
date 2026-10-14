package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// RelightPreintentProducer carries source-authorized original object locators.
// It is not evidence of bytes, raster pixels, qualification or admission.
type RelightPreintentProducer struct {
	Selected relightProducerLocator `json:"selected"`
	Root     relightProducerLocator `json:"root"`
	Params   relightProducerLocator `json:"params"`
}

// DiscoverRelightPreintentProducer retains source and producer locks in caller's
// scheduler transaction. Commit discovery before object IO, then repeat and
// compare every locator before using independently verified original bytes.
func DiscoverRelightPreintentProducer(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant, project, snapshot uuid.UUID, base v2input.SessionVersionBinding, selected v2input.Ref, kind, bucket string) (RelightPreintentProducer, error) {
	var zero RelightPreintentProducer
	if kind != "depth" && kind != "person" && kind != "manual_target" && kind != "manual_guard" {
		return zero, v2input.ErrInvalid
	}
	if bucket == "" || !validLocalBase(1, base, selected, selected, 0) {
		return zero, v2input.ErrInvalid
	}
	if err := LockRelightSource(ctx, tx, who, tenant, project, snapshot, base); err != nil {
		return zero, err
	}
	baseRaw, err := canonical.Marshal(base)
	if err != nil {
		return zero, err
	}
	selectedRaw, err := canonical.Marshal(selected)
	if err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v28_relight_preintent_producer($1,$2,$3,$4,$5,$6::jsonb,$7::jsonb,$8)", tenant, project, snapshot, who.UserID, who.SessionID, string(baseRaw), string(selectedRaw), kind).Scan(&raw); err != nil {
		return zero, err
	}
	var result RelightPreintentProducer
	if len(raw) < 2 || len(raw) > 49152 || json.Unmarshal(raw, &result) != nil {
		return zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	encoded, err := canonical.Marshal(result)
	if err != nil || !bytes.Equal(normalized, encoded) {
		return zero, v2input.ErrInvalid
	}
	for _, locator := range []relightProducerLocator{result.Selected, result.Root, result.Params} {
		if locator.ArtifactID == uuid.Nil || locator.Bucket != bucket || locator.Key == "" || locator.Version == "" || locator.Version == "null" || locator.Bytes < 2 || locator.Bytes > 1048576 || locator.Media != "application/json" {
			return zero, v2input.ErrInvalid
		}
		if _, err = domain.ParseDigest(locator.SHA256); err != nil {
			return zero, v2input.ErrInvalid
		}
	}
	if manualLocatorRef(result.Selected) != selected || result.Params.ArtifactID == result.Selected.ArtifactID || result.Params.ArtifactID == result.Root.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	if kind == "depth" || kind == "person" {
		if result.Root != result.Selected || result.Params.Bytes > 65536 {
			return zero, v2input.ErrInvalid
		}
	} else if result.Root.ArtifactID == result.Selected.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return result, nil
}
