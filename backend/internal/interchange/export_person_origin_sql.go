package interchange

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

// SQLExportPersonOriginRepository publishes a receipt only in the still-draft
// frozen export. The caller separately verifies its signature, original
// producer and physical object bytes before Complete.
type SQLExportPersonOriginRepository struct{ Export SQLExportDocumentStore }

func (s SQLExportPersonOriginRepository) scope(ctx context.Context, expected PortablePersonOriginExpectation,
	prepared PreparedPersonOriginAttachment) (CreateDocument, error) {
	var zero CreateDocument
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Export.DB == nil || s.Export.Tenant == uuid.Nil || s.Export.Intent.Request.Format != "photoeditor_bundle" ||
		expected.EntryID == uuid.Nil || !validImportRef(expected.Recipe) || !validImportRef(expected.Override) ||
		!validImportRef(expected.MaskRef) || !validImportRef(expected.MaskPixels) ||
		!portablePersonOriginSHA.MatchString(expected.ModelLockSHA) ||
		prepared.Ref.ArtifactID != uuid.NewSHA1(expected.EntryID, []byte("v30:person-origin:"+expected.MaskRef.ArtifactID.String())) ||
		prepared.Ref.SHA256 != rawSHA(prepared.Body) || len(prepared.Body) < 2 || len(prepared.Body) > 16384 ||
		prepared.Mapping.EntryID != expected.EntryID || prepared.Mapping.Recipe != expected.Recipe ||
		prepared.Mapping.Override != expected.Override || prepared.Mapping.MaskRef != expected.MaskRef ||
		prepared.Mapping.MaskPixels != expected.MaskPixels || prepared.Mapping.Path != prepared.Path ||
		prepared.Mapping.SHA != prepared.Ref.SHA256 ||
		prepared.Path != "origins/"+expected.EntryID.String()+"/"+expected.MaskRef.ArtifactID.String()+".json" {
		return zero, v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, s.Export.Intent, s.Export.Frozen.Body())
	if err != nil {
		return zero, err
	}
	planRef, planBody := compiled.PlanDocument()
	plan := CreateDocument{Ref: planRef, Body: planBody, Purpose: "v30_interchange_export_plan"}
	if err := s.Export.valid(ctx, s.Export.Intent.Identity, plan); err != nil {
		return zero, err
	}
	var parsed importPlanDocument
	if err := decodeImportDocument(planBody, &parsed); err != nil {
		return zero, err
	}
	matched := 0
	for _, entry := range parsed.Entries {
		if entry.ID == expected.EntryID {
			if entry.Binding == nil || *entry.Binding != expected.Source || entry.Recipe == nil || *entry.Recipe != expected.Recipe {
				return zero, v2input.ErrInvalid
			}
			matched++
		}
	}
	if matched != 1 || parsed.Direction != "export" || parsed.Format != "photoeditor_bundle" {
		return zero, v2input.ErrInvalid
	}
	return plan, ctx.Err()
}

func (s SQLExportPersonOriginRepository) ReservePersonOrigin(ctx context.Context,
	expected PortablePersonOriginExpectation, prepared PreparedPersonOriginAttachment) (artifactjson.Reservation, error) {
	plan, err := s.scope(ctx, expected, prepared)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	var result artifactjson.Reservation
	err = tenant.WithTenant(ctx, s.Export.DB, s.Export.Who, s.Export.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var outcome string
		var id, token *uuid.UUID
		var ready bool
		err := tx.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_v30_export_person_origin_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`,
			s.Export.Tenant, s.Export.Who.UserID, s.Export.Who.SessionID, s.Export.Key, s.Export.Intent.RequestSHA,
			string(plan.Body), expected.EntryID, expected.MaskRef.ArtifactID, prepared.Ref.ArtifactID,
			prepared.Ref.SHA256, len(prepared.Body), s.Export.Bucket).Scan(&outcome, &id, &token, &ready)
		if err != nil {
			return err
		}
		switch outcome {
		case "quota":
			return artifactjson.ErrQuota
		case "conflict":
			return artifactjson.ErrConflict
		case "created", "resumed", "replayed":
		default:
			return artifactjson.ErrStale
		}
		if id == nil || *id != prepared.Ref.ArtifactID || token == nil || *token == uuid.Nil || ready != (outcome == "replayed") {
			return objectstore.ErrIntegrity
		}
		result = artifactjson.Reservation{ArtifactID: *id, Token: *token}
		if ready {
			result.Ready = &artifactjson.Ref{ID: *id, SHA256: prepared.Ref.SHA256}
		}
		return nil
	})
	return result, err
}

func (s SQLExportPersonOriginRepository) CompletePersonOrigin(ctx context.Context,
	expected PortablePersonOriginExpectation, prepared PreparedPersonOriginAttachment,
	reservation artifactjson.Reservation, object objectstore.Object) (v2input.Ref, error) {
	plan, err := s.scope(ctx, expected, prepared)
	if err != nil {
		return v2input.Ref{}, err
	}
	key, err := objectstore.DocumentKey(s.Export.Tenant, s.Export.Intent.Identity.Run, prepared.Ref.ArtifactID)
	if err != nil || reservation.ArtifactID != prepared.Ref.ArtifactID || reservation.Token == uuid.Nil ||
		object.Key != key || object.SHA256 != prepared.Ref.SHA256 || object.Bytes != int64(len(prepared.Body)) ||
		object.ContentType != "application/json" || object.VersionID == "" || object.VersionID == "null" {
		return v2input.Ref{}, objectstore.ErrIntegrity
	}
	err = tenant.WithTenant(ctx, s.Export.DB, s.Export.Who, s.Export.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var accepted bool
		if err := tx.QueryRow(ctx, `SELECT photo.complete_v30_export_person_origin_document($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)`,
			s.Export.Tenant, s.Export.Who.UserID, s.Export.Who.SessionID, s.Export.Key, s.Export.Intent.RequestSHA,
			string(plan.Body), expected.EntryID, expected.MaskRef.ArtifactID, prepared.Ref.ArtifactID,
			prepared.Ref.SHA256, len(prepared.Body), s.Export.Bucket, reservation.Token, object.VersionID).Scan(&accepted); err != nil {
			return err
		}
		if !accepted {
			return artifactjson.ErrStale
		}
		return nil
	})
	if err != nil {
		return v2input.Ref{}, err
	}
	return prepared.Ref, nil
}

var _ ExportPersonOriginRepository = SQLExportPersonOriginRepository{}
