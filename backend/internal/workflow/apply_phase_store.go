package workflow

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/review"
)

// ApplyPhaseStore separates the existing Params compiler from storage without
// changing phase selection. Production uses ApplyStore; an operator measurement
// store must retain its own measured output objects and explicit decisions.
// This interface is package-private through its parameter types: no new client
// admission endpoint, registry bypass or qualification authority is introduced.
type ApplyPhaseStore interface {
	DefectOutputs(ctx context.Context, l ApplyLease) ([]applyOutput, error)
	Claim(ctx context.Context) (*ApplyLease, error)
	Renew(ctx context.Context, l ApplyLease) (bool, error)
	Advance(ctx context.Context, l ApplyLease, to string) (bool, error)
	SetGroupCount(ctx context.Context, l ApplyLease, count int) (bool, error)
	Fail(ctx context.Context, l ApplyLease, code string) (bool, error)
	FailTerminalPredecessor(ctx context.Context, l ApplyLease) (bool, error)
	Adopt(ctx context.Context, l ApplyLease) (bool, error)
	Inputs(ctx context.Context, l ApplyLease) ([]applyInput, error)
	Outputs(ctx context.Context, l ApplyLease, op, role string) ([]applyOutput, error)
	Artifact(ctx context.Context, l ApplyLease, ref review.ArtifactRef) (styleArtifact, error)
	Register(ctx context.Context, l ApplyLease, n applyNodePlan) (bool, error)
	RegisterV4Plan(ctx context.Context, l ApplyLease, plan []ApplyV4Stage) (bool, error)
	ProcessDefectReviews(ctx context.Context, l ApplyLease) (string, error)
	ProcessCandidateReviews(ctx context.Context, l ApplyLease) (string, error)
	ProcessDustReviews(ctx context.Context, l ApplyLease) (string, error)
	DustDecisions(ctx context.Context, l ApplyLease) ([]applyDustDecision, error)
	CandidateOutputs(ctx context.Context, l ApplyLease, phase string) ([]applyOutput, error)
	Materialize(ctx context.Context, l ApplyLease, t PreparedTask) (bool, error)
	conservativeProposalReport(ctx context.Context, l ApplyLease, proposal applyOutput) (styleArtifact, error)
	ProcessCullingReview(context.Context, ApplyLease) (bool, error)
	RegisterCullingReview(context.Context, ApplyLease, json.RawMessage) (bool, error)
	FailEditorPrecompose(context.Context, ApplyLease) (bool, error)
	RecordEditorPrecompose(context.Context, ApplyLease, review.ArtifactRef, string) (bool, error)
}

var _ ApplyPhaseStore = ApplyStore{}

func (s ApplyStore) ProcessCullingReview(ctx context.Context, l ApplyLease) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.process_apply_culling_review($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&ok)
	return ok, e
}
func (s ApplyStore) RegisterCullingReview(ctx context.Context, l ApplyLease, p json.RawMessage) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.register_apply_culling_review($1,$2,$3,$4::jsonb)`, l.TenantID, l.RunID, l.Token, string(p)).Scan(&ok)
	return ok, e
}
func (s ApplyStore) FailEditorPrecompose(ctx context.Context, l ApplyLease) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.fail_editor_precompose_profile($1,$2)`, l.RunID, l.Token).Scan(&ok)
	return ok, e
}
func (s ApplyStore) RecordEditorPrecompose(ctx context.Context, l ApplyLease, a review.ArtifactRef, canvas string) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.record_editor_precompose($1,$2,$3,$4,$5,$6)`, l.TenantID, l.RunID, l.Token, a.ID, a.SHA256, canvas).Scan(&ok)
	return ok, e
}
