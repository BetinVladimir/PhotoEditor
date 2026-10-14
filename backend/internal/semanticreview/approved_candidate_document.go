package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
)

// ReadApprovedCandidateDocument reads a mask or transform pinned by the original
// candidate, repeating authenticated producer metadata after bounded object IO.
// Typed geometry and mask binary validation remain separate requirements.
func ReadApprovedCandidateDocument(ctx context.Context, db ApprovedProtectionQueryer,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, transform bool, read PlanObjectReader) (PlanDocument, error) {
	empty := PlanDocument{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if db == nil || read == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return empty, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return empty, err
	}
	var selected *ApprovedEdit
	for i := range edits {
		if edits[i].TargetID == targetID {
			selected = &edits[i]
		}
	}
	if selected == nil {
		return empty, ErrInvalid
	}
	pin, contract, kind := selected.Mask, "MaskRef", "mask"
	if transform {
		pin, contract, kind = selected.Transform, "ResolvedTransform", "transform"
	}
	var candidateBody []byte
	for _, document := range prepared.documents {
		if document.Source.Contract == "TransferCandidate" && document.Source.Ref == selected.Candidate {
			candidateBody = append([]byte(nil), document.Body...)
		}
	}
	if len(candidateBody) < 2 || len(candidateBody) > 8388608 {
		return empty, ErrInvalid
	}
	candidateSum := sha256.Sum256(candidateBody)
	if hex.EncodeToString(candidateSum[:]) != selected.Candidate.SHA256 {
		return empty, ErrInvalid
	}
	var revision int64
	for _, target := range targets {
		if target.ID == targetID {
			revision = target.Revision
		}
	}
	args := []any{tenant, intent, actor, session, prepared.Sources.Revision, targetID, revision, selected.Source.AssetVersionID, selected.Candidate.ArtifactID, selected.Candidate.SHA256, string(candidateBody), kind}
	load := func() (PlanSource, error) {
		var source PlanSource
		err := db.QueryRow(ctx, "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_candidate_child($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", args...).Scan(&source.Contract, &source.Ref.ArtifactID, &source.Ref.SHA256, &source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType)
		if err != nil {
			return PlanSource{}, err
		}
		if source.Contract != contract || source.Ref != pin || source.Bytes < 2 || source.Bytes > 8388608 || source.Bucket == "" || source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
			return PlanSource{}, ErrInvalid
		}
		return source, nil
	}
	before, err := load()
	if err != nil {
		return empty, err
	}
	raw, err := read(ctx, before)
	if err != nil {
		return empty, err
	}
	if int64(len(raw)) != before.Bytes {
		return empty, ErrInvalid
	}
	raw = append([]byte(nil), raw...)
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != before.Bytes || hex.EncodeToString(sum[:]) != pin.SHA256 {
		return empty, ErrInvalid
	}
	after, err := load()
	if err != nil {
		return empty, err
	}
	if before != after {
		return empty, ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return PlanDocument{Source: before, Body: raw}, nil
}
