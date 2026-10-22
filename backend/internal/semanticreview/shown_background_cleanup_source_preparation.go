package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func LoadShownBackgroundCleanupSourceDocuments(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenant, intent uuid.UUID, scoped ScopedPreviewReview, request PreviewRequest, read PlanObjectReader,
	owner ExposureOwnerCosts, limits ExposureResourceEstimate, pin ExposureQualificationPin, params BackgroundCleanupPreviewParams) (ExposureTargetDocuments, []byte, error) {
	empty := ExposureTargetDocuments{}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	if db == nil || read == nil || !validExposureQualification(pin) {
		return empty, nil, ErrInvalid
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, scoped.Target.ID, request); err != nil {
		return empty, nil, err
	}
	review, err := PrepareReviewDocuments(scoped.Review.Sources, scoped.Review.documents, scoped.Review.modelSHA, scoped.Review.datasetSHA)
	if err != nil {
		return empty, nil, err
	}
	var candidateRaw []byte
	var candidateRef v2input.Ref
	for _, doc := range review.documents {
		if scoped.Target.CandidateArtifactID != nil && doc.Source.Ref.ArtifactID == *scoped.Target.CandidateArtifactID && doc.Source.Contract == "TransferCandidate" {
			candidateRaw = append([]byte(nil), doc.Body...)
			candidateRef = doc.Source.Ref
		}
	}
	candidate, err := v2input.DecodeSemanticCandidate(candidateRaw)
	if err != nil || candidate.Mask == nil || candidate.Transform == nil || candidateRef.SHA256 != request.CandidateSHA {
		return empty, nil, ErrInvalid
	}
	args := []any{tenant, intent, who.UserID, who.SessionID, request.ExpectedRevision, scoped.Target.ID,
		request.TargetRevision, scoped.Target.AssetVersionID, candidateRef.ArtifactID, candidateRef.SHA256}
	var protection v2input.Ref
	if err := db.QueryRow(ctx, "SELECT artifact_id,sha256 FROM photo.semantic_approved_protection_pin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)", args...).Scan(&protection.ArtifactID, &protection.SHA256); err != nil {
		return empty, nil, err
	}
	if _, _, err := FreezeBackgroundCleanupPreviewParams(params); err != nil {
		return empty, nil, err
	}
	// Reuse the exact displayed preview scope; copy qualification is independent
	// of the inference backend that produced it.
	if params.TenantID != tenant || params.IntentID != intent || params.TargetID != scoped.Target.ID ||
		params.WorkflowID != scoped.Workflow || params.WorkflowRevision != request.ExpectedRevision ||
		params.TargetRevision != request.TargetRevision || params.Plan != review.PlanRef ||
		params.Intent != review.IntentRef || params.SnapshotID != review.SnapshotID ||
		params.Candidate != candidateRef || params.Source != candidate.Target || params.Protection != protection ||
		review.Intent.Kind != "background_cleanup" || review.Intent.EV != nil || !review.Intent.PreservePersons {
		return empty, nil, ErrConflict
	}
	load := func(query string, extra []any, expected v2input.Ref, contract string) ([]byte, error) {
		all := append(append([]any(nil), args...), extra...)
		var doc PlanDocument
		if err := db.QueryRow(ctx, query, all...).Scan(&doc.Source.Contract, &doc.Source.Ref.ArtifactID, &doc.Source.Ref.SHA256, &doc.Source.Bytes,
			&doc.Source.Bucket, &doc.Source.Key, &doc.Source.Version, &doc.Source.MediaType); err != nil {
			return nil, err
		}
		if doc.Source.Contract != contract || doc.Source.Ref != expected || doc.Source.Bytes < 2 || doc.Source.Bytes > 1048576 {
			return nil, ErrInvalid
		}
		raw, err := read(ctx, doc.Source)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(raw)
		if int64(len(raw)) != doc.Source.Bytes || hex.EncodeToString(sum[:]) != expected.SHA256 {
			return nil, ErrInvalid
		}
		return append([]byte(nil), raw...), nil
	}
	fields := "contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type"
	imageQuery := "SELECT " + fields + " FROM photo.semantic_approved_image_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)"
	source, err := load(imageQuery, []any{false, params.Source.Source.ArtifactID, params.Source.Source.SHA256}, params.Source.Source, "WorkingImage")
	if err != nil {
		return empty, nil, err
	}
	reference := review.Intent.Reference.Source
	refRaw, err := load(imageQuery, []any{true, reference.ArtifactID, reference.SHA256}, reference, "WorkingImage")
	if err != nil {
		return empty, nil, err
	}
	childQuery := "SELECT " + fields + " FROM photo.semantic_approved_candidate_child($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)"
	mask, err := load(childQuery, []any{string(candidateRaw), "mask"}, *candidate.Mask, "MaskRef")
	if err != nil {
		return empty, nil, err
	}
	transform, err := load(childQuery, []any{string(candidateRaw), "transform"}, *candidate.Transform, "ResolvedTransform")
	if err != nil {
		return empty, nil, err
	}
	protected, err := load("SELECT "+fields+" FROM photo.semantic_approved_protection_object($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)",
		[]any{protection.ArtifactID, protection.SHA256}, protection, "MaskRef")
	if err != nil {
		return empty, nil, err
	}
	result := ExposureTargetDocuments{TargetID: scoped.Target.ID, Protection: protection,
		Documents: ExposureSourceDocuments{Source: source, Reference: refRaw, Mask: mask, Protection: protected}, OwnerCosts: owner, Limits: limits, Qualification: pin}
	var intentRaw []byte
	for _, doc := range review.documents {
		if doc.Source.Ref == params.Intent {
			intentRaw = doc.Body
		}
	}
	_, width, height, err := ResolveBackgroundCleanupPreviewDocuments(params, candidateRaw, intentRaw, result.Documents)
	if err != nil {
		return empty, nil, err
	}
	if _, err := CompileShownBackgroundCleanupAdoptionResources(width, height, owner, limits); err != nil {
		return empty, nil, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, scoped.Target.ID, request); err != nil {
		return empty, nil, err
	}
	return result, transform, nil
}
