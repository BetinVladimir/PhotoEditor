package delivery

import (
	"bytes"
	"context"
	"errors"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

// PublishDelegatedSigningParams keeps immutable object IO outside SQL and
// repeats parent/lineage authority at reserve and complete. A ready replay
// returns only after the SQL function confirms the exact ready artifact.
func PublishDelegatedSigningParams(ctx context.Context,
	store SQLDelegatedLineageStore, lineageRef v2input.Ref, lineageBody []byte,
	intent provenance.FrozenSigningIntent, task provenance.FrozenSigningTask,
	objects artifactjson.Objects) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil { return zero, err }
	if store.DB == nil || objects == nil { return zero, v2input.ErrInvalid }
	docs, err := BuildDelegatedSigningDraftDocuments(ctx, store.Scope, intent, task)
	if err != nil { return zero, err }
	pins, err := store.scopePins(lineageRef, lineageBody)
	if err != nil { return zero, err }
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return zero, err
	}
	ref := docs.ParamsRef
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err = store.DB.QueryRow(ctx,`SELECT outcome,artifact_id,token,ready
	 FROM photo.reserve_v31_delivery_signer_params(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11)`,
		store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
		store.Slot.PresetID, string(pins), int64(len(lineageBody)),
		string(docs.Params), ref.ArtifactID, ref.SHA256,
		int64(len(docs.Params)), store.Bucket).Scan(&outcome,&id,&token,&ready)
	if err != nil { return zero, err }
	switch outcome {
	case "quota": return zero, artifactjson.ErrQuota
	case "conflict": return zero, artifactjson.ErrConflict
	case "created", "resumed", "replayed":
	default: return zero, artifactjson.ErrStale
	}
	if id == nil || *id != ref.ArtifactID || token == nil ||
		*token == uuid.Nil || ready != (outcome == "replayed") {
		return zero, objectstore.ErrIntegrity
	}
	if ready { return ref, nil }
	key, err := objectstore.DocumentKey(store.Scope.TenantID,
		intent.RunID(), ref.ArtifactID)
	if err != nil { return zero, err }
	object, err := objects.PutImmutable(ctx, key, bytes.NewReader(docs.Params),
		int64(len(docs.Params)), ref.SHA256, "application/json")
	if err := ctx.Err(); err != nil { return zero, err }
	if errors.Is(err, objectstore.ErrExists) ||
		errors.Is(err, objectstore.ErrUnavailable) {
		object, err = objects.RecoverImmutable(ctx, key,
			int64(len(docs.Params)), ref.SHA256, "application/json")
	}
	if err != nil { return zero, err }
	if object.Key != key || object.VersionID == "" ||
		object.VersionID == "null" || len(object.VersionID) > 1024 ||
		object.SHA256 != ref.SHA256 || object.Bytes != int64(len(docs.Params)) ||
		object.ContentType != "application/json" {
		return zero, objectstore.ErrIntegrity
	}
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return zero, err
	}
	var accepted bool
	err = store.DB.QueryRow(ctx,`SELECT photo.complete_v31_delivery_signer_params(
	 $1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13)`,
		store.Lease.Expansion, store.Lease.Token, store.Slot.Ordinal,
		store.Slot.PresetID, string(pins), int64(len(lineageBody)),
		string(docs.Params), ref.ArtifactID, ref.SHA256,
		int64(len(docs.Params)), store.Bucket, *token,
		object.VersionID).Scan(&accepted)
	if err != nil { return zero, err }
	if !accepted { return zero, artifactjson.ErrStale }
	if err := store.VerifyReady(ctx, lineageRef, lineageBody); err != nil {
		return zero, err
	}
	return ref, nil
}
