package workflow

import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/review"
)

// OperatorCullingReview retains the explicit selection separately. All inputs
// remain measured: SQL's final selection filter is not a license to omit corpus
// execution, nor is this isolated selection a persisted accepted SQL review.
type OperatorCullingReview struct {
	Ledger          *OperatorApplyLedger
	Objects         *OperatorPhysicalObjects
	Actor, Snapshot uuid.UUID
	SnapshotDigest  string
	Decision        review.CullingDecision
}

func (r OperatorCullingReview) MaterializeOperatorReview(ctx context.Context, l ApplyLease, items []applyInput, _ []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	fail := func() (OperatorReviewResult, error) {
		return OperatorReviewResult{}, errors.New("operator culling measured projection binding invalid")
	}
	if r.Ledger == nil || r.Objects == nil || !r.Ledger.scope(l) || l.Phase != "cull_review" || len(items) != l.ItemCount || len(r.Ledger.cullingProjection) == 0 {
		return fail()
	}
	read := func(op, role string) (styleArtifact, error) {
		count := 0
		var a styleArtifact
		for id, node := range r.Ledger.plans {
			if node.Operation == op {
				for _, p := range r.Ledger.outputs[id] {
					if p.Role == role {
						task, ok := r.Ledger.tasks[id]
						if !ok || p.Producer != id {
							return a, errors.New("culling producer changed")
						}
						if e := r.Objects.VerifyMeasuredObject(ctx, task, p); e != nil {
							return a, e
						}
						a = p.Artifact
						count++
					}
				}
			}
		}
		if count != 1 {
			return a, errors.New("culling unique measured producer missing")
		}
		return a, nil
	}
	groups, e := read("collection.group", "scene_groups")
	if e != nil {
		return OperatorReviewResult{}, e
	}
	proposal, e := read("collection.cull", "cull_recommendations")
	if e != nil {
		return OperatorReviewResult{}, e
	}
	gr, e := readApplyJSONBound(ctx, r.Objects, "operator-measurement", groups, 8<<20)
	if e != nil {
		return OperatorReviewResult{}, e
	}
	pr, e := readApplyJSONBound(ctx, r.Objects, "operator-measurement", proposal, 8<<20)
	if e != nil {
		return OperatorReviewResult{}, e
	}
	projection, e := applyCullingProjection(items, gr, pr, groups.ref(), proposal.ref())
	if e != nil {
		return OperatorReviewResult{}, e
	}
	if !bytes.Equal(projection, r.Ledger.cullingProjection) {
		return fail()
	}
	plan, e := review.PlanOperatorCulling(review.OperatorCullingScope{Tenant: l.TenantID, Actor: r.Actor, Run: l.RunID, Review: uuid.NewSHA1(l.RunID, []byte("operator-culling")), Snapshot: r.Snapshot, SnapshotDigest: r.SnapshotDigest, CandidateHash: proposal.SHA256, Projection: projection}, r.Decision)
	if e != nil {
		return OperatorReviewResult{}, e
	}
	return OperatorReviewResult{Phase: l.Phase, DecisionSHA256: plan.DecisionHash, Culling: &plan}, nil
}

var _ OperatorReviewMaterializer = OperatorCullingReview{}
