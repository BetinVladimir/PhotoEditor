package workflow

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
)

// ReviewExpansionGate durably requests scheduler expansion after a defect
// decision. It never marks an unmaterialized repair task ready. The compiler
// must consume the open barrier, persist the selected branch and seal it before
// dispatch can resume. Install only with that compiler enabled.
type ReviewExpansionGate struct{}

var _ review.DefectGate = ReviewExpansionGate{}

func (ReviewExpansionGate) ApplyDefectDecision(ctx context.Context, tx pgx.Tx, s review.DefectSnapshot, p review.DefectPlan) error {
	if tx == nil || p.Revision != s.Revision+1 || p.Selection.Revision != p.Revision || p.Selection.Proposal != s.Proposal ||
		p.Selection.Image != s.Image || p.Selection.LabelMap != s.LabelMap {
		return domain.NewError("INTERNAL_ERROR", 500)
	}
	if _, err := domain.ParseDigest(p.DecisionHash); err != nil {
		return domain.NewError("INTERNAL_ERROR", 500)
	}
	expected := 0
	switch p.Route {
	case "repair":
		if len(p.Selection.Accepted) == 0 {
			return domain.NewError("INTERNAL_ERROR", 500)
		}
		expected = 1
	case "base", "skip":
		if len(p.Selection.Accepted) != 0 {
			return domain.NewError("INTERNAL_ERROR", 500)
		}
	default:
		return domain.NewError("INTERNAL_ERROR", 500)
	}
	// Kind identifies one immutable review; cursor remains owned by expansion.
	kind := fmt.Sprintf("review.defects/%s", s.ReviewID)
	tag, err := tx.Exec(ctx, `WITH decision AS (
 SELECT v.tenant_id,v.run_id,v.task_id FROM photo.reviews v
 JOIN photo.runs r ON r.tenant_id=v.tenant_id AND r.id=v.run_id
 WHERE v.tenant_id=$1 AND v.id=$2 AND v.run_id=$3 AND v.kind='defects'
 AND v.state='decided' AND v.revision=$4 AND v.decision_hash=$5 AND v.decision_route=$6
 AND v.task_id IS NOT NULL AND NOT r.cancel_requested AND r.status IN ('running','waiting_review')
 AND r.revision<9007199254740991 FOR UPDATE OF r,v
 ), expansion AS (
 INSERT INTO photo.graph_expansions(tenant_id,run_id,parent_task_id,kind,state,expected_count)
 SELECT tenant_id,run_id,task_id,$7,'open',$8 FROM decision
 RETURNING tenant_id,run_id
 ) UPDATE photo.runs r SET graph_sealed=false,status='running',revision=revision+1
 FROM expansion e WHERE r.tenant_id=e.tenant_id AND r.id=e.run_id`,
		s.TenantID, s.ReviewID, s.RunID, p.Revision, p.DecisionHash, p.Route, kind, expected)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return domain.NewError("REVISION_CONFLICT", 409)
	}
	return nil
}
