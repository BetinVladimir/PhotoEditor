package semanticreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// VerifyApprovedProtectionOrigin checks exact typed root bytes and authenticated
// canonical producer authority. It returns the proven binary pin, not download
// authorization or numerical NPY validation. Repeat in final admission scope.
func VerifyApprovedProtectionOrigin(ctx context.Context, db ApprovedProtectionQueryer,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget, request ApplyDecision,
	targetID uuid.UUID, document PlanDocument) (v2input.Ref, error) {
	empty := v2input.Ref{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	source := document.Source
	if db == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil || source.Contract != "MaskRef" || source.Ref.ArtifactID == uuid.Nil || !shaPattern.MatchString(source.Ref.SHA256) || source.Bytes < 2 || source.Bytes > 8388608 || int64(len(document.Body)) != source.Bytes || source.Bucket == "" || source.Key == "" || source.Version == "" || source.Version == "null" || source.MediaType != "application/json" {
		return empty, ErrInvalid
	}
	raw := append([]byte(nil), document.Body...)
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != source.Ref.SHA256 {
		return empty, ErrInvalid
	}
	mask, err := review.ParseMaskRef(raw)
	if err != nil || !mask.Protected {
		return empty, ErrInvalid
	}
	binary := v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}
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
	var revision int64
	for _, target := range targets {
		if target.ID == targetID {
			revision = target.Revision
		}
	}
	var ready bool
	if err := db.QueryRow(ctx, "SELECT photo.semantic_approved_protection_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)", tenant, intent, actor, session, prepared.Sources.Revision, targetID, revision, selected.Source.AssetVersionID, selected.Candidate.ArtifactID, selected.Candidate.SHA256, source.Ref.ArtifactID, source.Ref.SHA256, binary.ArtifactID, binary.SHA256, string(raw)).Scan(&ready); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if !ready {
		return empty, ErrConflict
	}
	return binary, nil
}
