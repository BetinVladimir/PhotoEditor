package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type defectSkinParseObjects struct{ Manifest, Params, Pixels defectEvidenceObject }

func (s SQLTransferSourceReader) defectSkinObjects(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, pixel, parse v2input.Ref) (defectSkinParseObjects, error) {
	empty := defectSkinParseObjects{}
	if s.Bucket == "" {
		return empty, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{pixel, parse} {
		if ref.ArtifactID == uuid.Nil {
			return empty, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return empty, v2input.ErrInvalid
		}
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return empty, err
	}
	var result defectSkinParseObjects
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, "SELECT contract,id,sha,bucket,object_key,object_version,bytes FROM photo.semantic_proposal_defect_skin_parse_objects($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22)", tenantID, s.ProjectID, who.UserID, who.SessionID, binding.AssetVersionID, binding.Source.ArtifactID, binding.Source.SHA256, pins.Proposal.ArtifactID, pins.Proposal.SHA256, pins.Report.ArtifactID, pins.Report.SHA256, pins.Labels.ArtifactID, pins.Labels.SHA256, pins.Protection.ArtifactID, pins.Protection.SHA256, pins.Params.ArtifactID, pins.Params.SHA256, pixel.ArtifactID, pixel.SHA256, "skin", parse.ArtifactID, parse.SHA256)
		if err != nil {
			return err
		}
		defer rows.Close()
		seen := map[string]bool{}
		ids := map[uuid.UUID]bool{}
		for rows.Next() {
			var role, bucket string
			var entry defectEvidenceObject
			if err := rows.Scan(&role, &entry.ref.ArtifactID, &entry.ref.SHA256, &bucket, &entry.object.Key, &entry.object.VersionID, &entry.object.Bytes); err != nil {
				return err
			}
			if seen[role] || ids[entry.ref.ArtifactID] {
				return v2input.ErrConflict
			}
			seen[role] = true
			ids[entry.ref.ArtifactID] = true
			if entry.ref.ArtifactID == uuid.Nil || bucket != s.Bucket || entry.object.Key == "" || entry.object.VersionID == "" || entry.object.VersionID == "null" {
				return v2input.ErrInvalid
			}
			if _, err := domain.ParseDigest(entry.ref.SHA256); err != nil {
				return v2input.ErrInvalid
			}
			minimum, maximum := int64(2), int64(1048576)
			entry.object.ContentType = "application/json"
			if role == "pixels" {
				minimum, maximum = 32, 400065548
				entry.object.ContentType = "application/octet-stream"
			}
			if entry.object.Bytes < minimum || entry.object.Bytes > maximum {
				return v2input.ErrInvalid
			}
			entry.object.SHA256 = entry.ref.SHA256
			switch role {
			case "manifest":
				result.Manifest = entry
			case "params":
				result.Params = entry
			case "pixels":
				result.Pixels = entry
			default:
				return v2input.ErrInvalid
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if len(seen) != 3 || result.Manifest.ref != parse {
			return v2input.ErrInvalid
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return result, nil
}
