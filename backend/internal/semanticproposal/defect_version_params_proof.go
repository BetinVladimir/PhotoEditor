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
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type defectVersionParamsProof struct {
	document    uuid.UUID
	params      v2input.Ref
	object      objectstore.Object
	preparation defectManualParamsPreparation
}

func (s SQLTransferSourceReader) defectVersionParamsObject(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, document uuid.UUID, params v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || document == uuid.Nil || params.ArtifactID == uuid.Nil {
		return object, v2input.ErrInvalid
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var id uuid.UUID
		var bucket string
		err := tx.QueryRow(ctx, "SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_defect_version_params_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, document, params.ArtifactID, params.SHA256).Scan(&id, &object.SHA256, &bucket, &object.Key, &object.VersionID, &object.Bytes, &object.ContentType)
		if err != nil {
			return err
		}
		if id != params.ArtifactID || bucket != s.Bucket || object.SHA256 != params.SHA256 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.Bytes < 2 || object.Bytes > 1048576 || object.ContentType != "application/json" {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return objectstore.Object{}, err
	}
	return object, nil
}

// Retains exact newly published Params metadata and native preparation; no queue
// authority is granted. Reads only the pinned object version and repeats scope.
func (s SQLTransferSourceReader) readDefectVersionParamsProof(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, document uuid.UUID, published PublishedDefectParams, prepared defectManualParamsPreparation) (defectVersionParamsProof, error) {
	var zero defectVersionParamsProof
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	var inputs, roots []review.ArtifactRef
	if json.Unmarshal([]byte(prepared.params.inputs), &inputs) != nil || json.Unmarshal([]byte(prepared.params.roots), &roots) != nil {
		return zero, v2input.ErrInvalid
	}
	a, err := canonical.Marshal(struct{ Inputs, Roots []review.ArtifactRef }{inputs, roots})
	if err != nil {
		return zero, err
	}
	b, err := canonical.Marshal(struct{ Inputs, Roots []review.ArtifactRef }{published.Inputs, published.ManualRoots})
	if err != nil || string(a) != string(b) || published.Ref.SHA256 != prepared.params.digest {
		return zero, v2input.ErrInvalid
	}
	if err := s.recheckManualDefectParams(ctx, who, tenantID, binding, color, prepared); err != nil {
		return zero, err
	}
	object, err := s.defectVersionParamsObject(ctx, who, tenantID, binding, document, published.Ref)
	if err != nil {
		return zero, err
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, published.Ref)
	if err != nil {
		return zero, err
	}
	if string(body) != prepared.params.params {
		return zero, v2input.ErrConflict
	}
	current, err := s.defectVersionParamsObject(ctx, who, tenantID, binding, document, published.Ref)
	if err != nil {
		return zero, err
	}
	if current != object {
		return zero, v2input.ErrConflict
	}
	if err := s.recheckManualDefectParams(ctx, who, tenantID, binding, color, prepared); err != nil {
		return zero, err
	}
	return defectVersionParamsProof{document: document, params: published.Ref, object: object, preparation: prepared}, nil
}

func (s SQLTransferSourceReader) lockDefectVersionParamsProof(ctx context.Context, tx pgx.Tx, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, proof defectVersionParamsProof) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	source := proof.preparation.source
	if tx == nil || proof.document == uuid.Nil || proof.params.ArtifactID == uuid.Nil || proof.params.SHA256 != proof.preparation.params.digest || proof.object.SHA256 != proof.params.SHA256 || proof.object.Key == "" || proof.object.VersionID == "" || proof.object.VersionID == "null" || proof.object.Bytes != int64(len(proof.preparation.params.params)) || proof.object.ContentType != "application/json" || source.tenant != tenantID || source.project != s.ProjectID || source.actor != who.UserID || source.session != who.SessionID || source.binding != binding || source.bucket != s.Bucket || !source.unitRGB {
		return v2input.ErrInvalid
	}
	if _, err := marshalSourceEvidencePins(source); err != nil {
		return err
	}
	if err := s.lockDefectExecutionAuthorization(ctx, tx, who, tenantID, binding, source); err != nil {
		return err
	}
	pins, err := canonical.Marshal(map[string]any{"key": proof.object.Key, "version": proof.object.VersionID, "sha256": proof.object.SHA256, "bytes": proof.object.Bytes, "media_type": proof.object.ContentType, "bucket": s.Bucket})
	if err != nil {
		return err
	}
	var ready bool
	if err := tx.QueryRow(ctx, "SELECT photo.lock_semantic_defect_version_params($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, proof.document, proof.params.ArtifactID, proof.params.SHA256, string(pins)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrConflict
	}
	return ctx.Err()
}
