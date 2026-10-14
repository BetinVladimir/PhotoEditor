package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// Retains exact published Params object pin for accepted-version ownership lock.
func marshalPersonParamsPin(ctx context.Context, bucket string, prepared sourcePersonPreparation) ([]byte, error) {
	if prepared.document == uuid.Nil || bucket == "" {
		return nil, v2input.ErrInvalid
	}
	evidence, err := decodePersonInputEvidence(ctx, prepared.catalog.frozen, prepared.catalog.inputs)
	if err != nil {
		return nil, err
	}
	draft, err := prepared.catalog.frozen.DraftTask()
	if err != nil {
		return nil, err
	}
	bytes, err := prepared.catalog.frozen.ParamsByteCount()
	if err != nil {
		return nil, err
	}
	var pin []byte
	for _, entry := range evidence {
		if entry.Contract != "Params" {
			continue
		}
		obj := entry.Object
		if pin != nil || entry.Ref.ArtifactID != draft.ParamsArtifactID || entry.Ref.SHA256 != draft.ParamsSHA || obj.Bytes != bytes || obj.ContentType != "application/json" {
			return nil, v2input.ErrInvalid
		}
		pin, err = canonical.Marshal(map[string]any{"key": obj.Key, "version": obj.VersionID, "sha256": obj.SHA256, "bytes": obj.Bytes, "media_type": obj.ContentType, "bucket": bucket})
		if err != nil {
			return nil, err
		}
	}
	if pin == nil {
		return nil, v2input.ErrInvalid
	}
	return pin, nil
}

// Caller owns scheduler transaction and rolls back any failure. This locks only
// Params ownership/object; source/qualification/input registration are separate.
func (s SQLTransferSourceReader) lockPersonParams(ctx context.Context, tx pgx.Tx, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, prepared sourcePersonPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	pin, err := marshalPersonParamsPin(ctx, s.Bucket, prepared)
	if err != nil {
		return err
	}
	draft, err := prepared.catalog.frozen.DraftTask()
	if err != nil {
		return err
	}
	var role string
	if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	err = tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", tenant, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, prepared.document, draft.ParamsArtifactID, draft.ParamsSHA, string(pin)).Scan(&ready)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
