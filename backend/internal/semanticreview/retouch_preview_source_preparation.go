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

func LoadRetouchPreviewSourceDocuments(ctx context.Context, db *pgxpool.Pool, who auth.Identity,
	tenant, intent uuid.UUID, scoped ScopedPreviewReview, request PreviewRequest, read PlanObjectReader,
	owner ExposureOwnerCosts, limits ExposureResourceEstimate, pin RetouchQualificationPin) (RetouchTargetDocuments, []byte, error) {
	empty := RetouchTargetDocuments{}
	if err := ctx.Err(); err != nil {
		return empty, nil, err
	}
	if db == nil || read == nil || !validRetouchQualification(pin) || limits.VRAMBytes != 0 {
		return empty, nil, ErrInvalid
	}
	if _, err := CompileRetouchPreviewResources(1, 1, owner, limits); err != nil {
		return empty, nil, err
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
	params, err := CompileRetouchPreviewParams(tenant, intent, scoped.Workflow, review, scoped.Target, request, protection, pin)
	if err != nil {
		return empty, nil, err
	}
	load := func(query string, extra []any, expected v2input.Ref, contract string) ([]byte, error) {
		all := append(append([]any(nil), args...), extra...)
		var doc PlanDocument
		if err := db.QueryRow(ctx, query, all...).Scan(&doc.Source.Contract, &doc.Source.Ref.ArtifactID, &doc.Source.Ref.SHA256, &doc.Source.Bytes,
			&doc.Source.Bucket, &doc.Source.Key, &doc.Source.Version, &doc.Source.MediaType); err != nil {
			return nil, err
		}
		return readRetouchPreviewSource(ctx, doc.Source, expected, contract, read)
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
	result := RetouchTargetDocuments{TargetID: scoped.Target.ID, Protection: protection,
		Documents: ExposureSourceDocuments{Source: source, Reference: refRaw, Mask: mask, Protection: protected}, OwnerCosts: owner, Limits: limits, Qualification: pin}
	var intentRaw []byte
	for _, doc := range review.documents {
		if doc.Source.Ref == params.Intent {
			intentRaw = doc.Body
		}
	}
	_, width, height, err := ResolveRetouchPreviewDocuments(params, candidateRaw, intentRaw, result.Documents)
	if err != nil {
		return empty, nil, err
	}
	if _, err := CompileRetouchPreviewResources(width, height, owner, limits); err != nil {
		return empty, nil, err
	}
	if err := CheckCurrentPreviewAuthority(ctx, db, who, tenant, intent, scoped.Target.ID, request); err != nil {
		return empty, nil, err
	}
	return result, transform, nil
}

// readRetouchPreviewSource owns a verified versioned document before the next
// storage callback can reuse its buffer. Authority is checked by the SQL caller
// and again after the complete source inventory has been resolved.
func readRetouchPreviewSource(ctx context.Context, source PlanSource, expected v2input.Ref, contract string, read PlanObjectReader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if read == nil || expected.ArtifactID == uuid.Nil || source.Contract != contract || source.Ref != expected ||
		source.Bytes < 2 || source.Bytes > 1048576 || source.Bucket == "" || source.Key == "" ||
		source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
		return nil, ErrInvalid
	}
	raw, err := read(ctx, source)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != source.Bytes || hex.EncodeToString(sum[:]) != expected.SHA256 {
		return nil, ErrInvalid
	}
	return append([]byte(nil), raw...), nil
}
