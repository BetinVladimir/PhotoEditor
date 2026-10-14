package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type personEvidenceObject struct {
	contract string
	ref      v2input.Ref
	object   objectstore.Object
	backend  string
}

func (s SQLTransferSourceReader) personEvidenceObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, mask VerifiedPersonMask) ([]personEvidenceObject, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || s.ProjectID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	var result []personEvidenceObject
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT contract,artifact_id,sha256,bucket,object_key,object_version,bytes,backend FROM photo.semantic_proposal_person_evidence_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, mask.Root.ArtifactID, mask.Root.SHA256, mask.Pixels.ArtifactID, mask.Pixels.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var e personEvidenceObject
			var bucket string
			if err := rows.Scan(&e.contract, &e.ref.ArtifactID, &e.ref.SHA256, &bucket, &e.object.Key, &e.object.VersionID, &e.object.Bytes, &e.backend); err != nil {
				return err
			}
			if len(result) >= 2 || bucket != s.Bucket {
				return v2input.ErrInvalid
			}
			e.object.SHA256 = e.ref.SHA256
			e.object.ContentType = "application/json"
			result = append(result, e)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(result) != 2 || result[0].contract != "PersonParams" || result[1].contract != "PersonReport" || result[0].ref.ArtifactID == result[1].ref.ArtifactID || result[0].backend != result[1].backend {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// ReadPersonEvidence reads exact canonical producer Params/report, validates
// consistency and repeats live SQL pins after IO. Qualification and complete
// policy selection are separate; this returns no activation certificate.
func (s SQLTransferSourceReader) ReadPersonEvidence(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root v2input.Ref, modelLock, backend string) (VerifiedPersonMask, error) {
	mask, _, err := s.readPersonEvidencePinned(ctx, who, tenantID, binding, root, modelLock, backend)
	return mask, err
}

func (s SQLTransferSourceReader) readPersonEvidencePinned(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, root v2input.Ref, modelLock, backend string) (VerifiedPersonMask, []personEvidenceObject, error) {
	empty := VerifiedPersonMask{}
	if backend != "cpu" && backend != "cuda" && backend != "mps" {
		return empty, nil, v2input.ErrInvalid
	}
	mask, err := s.ReadPersonMask(ctx, who, tenantID, binding, root, modelLock)
	if err != nil {
		return empty, nil, err
	}
	pins, err := s.personEvidenceObjects(ctx, who, tenantID, binding, mask)
	if err != nil {
		return empty, nil, err
	}
	if pins[0].backend != backend {
		return empty, nil, v2input.ErrInvalid
	}
	image, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return empty, nil, err
	}
	bodies := make([][]byte, 2)
	for i, pin := range pins {
		bodies[i], err = v2input.ReadPinnedSemanticDocument(ctx, s.Objects, pin.object, pin.ref)
		if err != nil {
			return empty, nil, err
		}
	}
	if err := v2input.VerifySemanticPersonEvidence(binding, image, root, mask.Body, bodies[0], bodies[1], modelLock, backend); err != nil {
		return empty, nil, err
	}
	current, err := s.personEvidenceObjects(ctx, who, tenantID, binding, mask)
	if err != nil {
		return empty, nil, err
	}
	for i, pin := range pins {
		if pin != current[i] {
			return empty, nil, v2input.ErrConflict
		}
	}
	return mask, pins, nil
}
