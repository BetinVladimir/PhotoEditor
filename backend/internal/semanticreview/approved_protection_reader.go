package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
)

type ApprovedProtectionQueryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// ReadApprovedProtectionPins rebuilds approvals from private review before any
// SQL. Discovery repeats live session/revision/target/candidate scope per target;
// returned pins do not authorize object reads or prove binary producer origin.
// No partial map is returned on refusal, missing pin, cancellation or error.
func ReadApprovedProtectionPins(ctx context.Context, db ApprovedProtectionQueryer,
	tenant, intent, actor, session uuid.UUID, prepared PreparedReview, targets []LockedTarget,
	request ApplyDecision) (map[uuid.UUID]v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if db == nil || tenant == uuid.Nil || intent == uuid.Nil || actor == uuid.Nil || session == uuid.Nil {
		return nil, ErrInvalid
	}
	edits, err := CompileApprovedEdits(prepared, targets, request)
	if err != nil {
		return nil, err
	}
	revisions := make(map[uuid.UUID]int64, len(targets))
	for _, target := range targets {
		revisions[target.ID] = target.Revision
	}
	result := make(map[uuid.UUID]v2input.Ref, len(edits))
	for _, edit := range edits {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var pin v2input.Ref
		if err := db.QueryRow(ctx, "SELECT artifact_id,sha256 FROM photo.semantic_approved_protection_pin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)",
			tenant, intent, actor, session, prepared.Sources.Revision, edit.TargetID, revisions[edit.TargetID], edit.Source.AssetVersionID, edit.Candidate.ArtifactID, edit.Candidate.SHA256).Scan(&pin.ArtifactID, &pin.SHA256); err != nil {
			return nil, err
		}
		if pin.ArtifactID == uuid.Nil || !shaPattern.MatchString(pin.SHA256) {
			return nil, ErrInvalid
		}
		result[edit.TargetID] = pin
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
