package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// DB-only component. Caller composes all qualification/policy/graph admission
// and rolls back the complete transaction on refusal. No object IO here.
func (s SQLTransferSourceReader) lockNativeSelectionSeparation(ctx context.Context, tx pgx.Tx, who auth.Identity, request Request, proof nativeSelectionSeparationProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	pins, err := s.marshalNativeSelectionPins(who, request, proof)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	binding := request.Intent.Reference
	if err := s.lockNativeDefectContinuation(ctx, tx, who, request.TenantID, binding, proof.registered.parent); err != nil {
		return err
	}
	objects := proof.registered.objects
	if objects[1] != proof.registered.parent.result.origin.objects[4] || objects[0].ref.ArtifactID == uuid.Nil {
		return v2input.ErrInvalid
	}
	entries := make([]map[string]any, 2)
	for i, pin := range objects {
		entries[i] = map[string]any{"ordinal": i, "artifact_id": pin.ref.ArtifactID, "sha256": pin.ref.SHA256, "bucket": s.Bucket, "key": pin.object.Key, "version": pin.object.VersionID, "bytes": pin.object.Bytes, "media_type": pin.object.ContentType}
	}
	raw, err := canonical.Marshal(entries)
	if err != nil {
		return err
	}
	native := proof.registered.parent.result
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_protection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12,$13::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, native.task, native.catalog.ResultID, proof.registered.parent.anchor, objects[0].ref.ArtifactID, objects[0].ref.SHA256, string(raw)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_native_selection_evidence($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::jsonb)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, binding.CanvasHash, proof.root.ArtifactID, proof.root.SHA256, request.Intent.TargetSnapshotID, string(pins)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}

// The original intent digest binds selection/source/snapshot to the parent.
func (s SQLTransferSourceReader) marshalNativeSelectionPins(who auth.Identity, request Request, proof nativeSelectionSeparationProof) ([]byte, error) {
	var anchor struct {
		Intent     v2input.Ref                   `json:"intent"`
		Source     v2input.SessionVersionBinding `json:"source"`
		ParentRun  uuid.UUID                     `json:"parent_run_id"`
		RequestSHA string                        `json:"request_sha"`
		Session    uuid.UUID                     `json:"auth_session_id"`
	}
	sha, err := canonical.Hash(request.Intent)
	if err != nil || s.Bucket == "" || s.ProjectID == uuid.Nil || who.UserID != request.ActorID || who.SessionID == uuid.Nil || request.TenantID == uuid.Nil || proof.count <= 0 || proof.root != request.Intent.ReferenceMask || proof.root.ArtifactID == uuid.Nil || proof.pixels.ArtifactID == uuid.Nil || proof.root.ArtifactID == proof.pixels.ArtifactID || json.Unmarshal([]byte(proof.registered.parent.anchor), &anchor) != nil || anchor.Intent.SHA256 != sha || anchor.Source != request.Intent.Reference || anchor.ParentRun != request.RunID || anchor.RequestSHA != request.RequestSHA || anchor.Session != who.SessionID {
		return nil, v2input.ErrInvalid
	}
	return s.marshalSelectionObjectPins(proof.root, proof.pixels, proof.rootObject, proof.pixelsObject)
}

func (s SQLTransferSourceReader) marshalSelectionObjectPins(root, pixels v2input.Ref, rootObject, pixelsObject objectstore.Object) ([]byte, error) {
	entries := make([]map[string]any, 2)
	for i, pin := range []struct {
		ref    v2input.Ref
		object objectstore.Object
	}{{root, rootObject}, {pixels, pixelsObject}} {
		if _, err := domain.ParseDigest(pin.ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		if pin.object.SHA256 != pin.ref.SHA256 || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" || pin.object.Bytes <= 0 {
			return nil, v2input.ErrInvalid
		}
		contract := "MaskRef"
		var parentID, parentSHA any
		if i == 0 {
			if pin.object.ContentType != "application/json" {
				return nil, v2input.ErrInvalid
			}
		} else {
			contract = "MaskPixels"
			parentID = root.ArtifactID
			parentSHA = root.SHA256
			if pin.object.ContentType != "application/x-npy" && pin.object.ContentType != "application/octet-stream" {
				return nil, v2input.ErrInvalid
			}
		}
		entries[i] = map[string]any{"contract": contract, "artifact_id": pin.ref.ArtifactID, "sha256": pin.ref.SHA256, "bucket": s.Bucket, "key": pin.object.Key, "version": pin.object.VersionID, "bytes": pin.object.Bytes, "media_type": pin.object.ContentType, "parent_id": parentID, "parent_sha": parentSHA}
	}
	return canonical.Marshal(entries)
}
