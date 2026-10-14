package semanticproposal

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// Reads only a registered derived root for this exact owned parent request.
// Full native reads prove original NPY bytes; root bytes must match the report's
// original inline mask. Qualification/final transfer admission remain separate.
type registeredNativeProtectionProof struct {
	objects [2]defectEvidenceObject
	parent  nativeDefectParentProof
}

func (s SQLTransferSourceReader) ReadRegisteredNativeDefectProtection(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root v2input.Ref) (VerifiedProtection, error) {
	result, _, err := s.readRegisteredNativeDefectProtection(ctx, who, request, key, intent, snapshot, task, root)
	return result, err
}

func (s SQLTransferSourceReader) readRegisteredNativeDefectProtection(ctx context.Context, who auth.Identity, request Request, key string, intent, snapshot v2input.Ref, task uuid.UUID, root v2input.Ref) (VerifiedProtection, registeredNativeProtectionProof, error) {
	var zero VerifiedProtection
	result, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	lookup := func() ([2]defectEvidenceObject, error) {
		return s.registeredNativeProtectionObjects(ctx, who, request, task, root, result.parentProof)
	}
	before, err := lookup()
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, before[0].object, root)
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	image, err := s.Read(ctx, who, request.TenantID, request.Intent.Reference)
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	expected, pixels, err := v2input.BuildSemanticDefectProtectionDocument(ctx, result.Pins.Params, result.Evidence.Params, result.Pins.Report, result.Evidence.Report, result.Pins.Proposal, result.Evidence.Proposal, request.Intent.Reference, image)
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	if !bytes.Equal(raw, expected) || pixels != before[1].ref {
		return zero, registeredNativeProtectionProof{}, v2input.ErrInvalid
	}
	after, err := lookup()
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	if after != before {
		return zero, registeredNativeProtectionProof{}, v2input.ErrConflict
	}
	current, err := s.ReadNativeDefectResultForRequest(ctx, who, request, key, intent, snapshot, task)
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	if current.parentProof != result.parentProof {
		return zero, registeredNativeProtectionProof{}, v2input.ErrConflict
	}
	finalPins, err := lookup()
	if err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	if finalPins != before {
		return zero, registeredNativeProtectionProof{}, v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, registeredNativeProtectionProof{}, err
	}
	return VerifiedProtection{Root: root, Pixels: pixels, Body: raw}, registeredNativeProtectionProof{objects: before, parent: result.parentProof}, nil
}

func (s SQLTransferSourceReader) registeredNativeProtectionObjects(ctx context.Context, who auth.Identity, request Request, task uuid.UUID, root v2input.Ref, proof nativeDefectParentProof) ([2]defectEvidenceObject, error) {
	var pins [2]defectEvidenceObject
	err := tenant.WithTenant(ctx, s.DB, who, request.TenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT ordinal,artifact_id,sha256,bucket,object_key,object_version,bytes,media_type FROM photo.semantic_native_protection_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11,$12)", request.TenantID, s.ProjectID, who.UserID, who.SessionID, request.Intent.Reference.AssetVersionID, request.Intent.Reference.Source.ArtifactID, request.Intent.Reference.Source.SHA256, task, proof.result.catalog.ResultID, proof.anchor, root.ArtifactID, root.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		count := 0
		for rows.Next() {
			if count >= 2 {
				return v2input.ErrInvalid
			}
			var ordinal int
			var bucket string
			pin := &pins[count]
			if err := rows.Scan(&ordinal, &pin.ref.ArtifactID, &pin.ref.SHA256, &bucket, &pin.object.Key, &pin.object.VersionID, &pin.object.Bytes, &pin.object.ContentType); err != nil {
				return err
			}
			if ordinal != count || bucket != s.Bucket || pin.object.Key == "" || pin.object.VersionID == "" || pin.object.VersionID == "null" {
				return v2input.ErrInvalid
			}
			pin.object.SHA256 = pin.ref.SHA256
			count++
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if count != 2 || pins[0].ref != root || pins[1] != proof.result.origin.objects[4] {
			return v2input.ErrConflict
		}
		return nil
	})
	if err != nil {
		return [2]defectEvidenceObject{}, err
	}
	return pins, nil
}
