package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestSourceVerifiedDraftRejectsForeignScopeBeforeSQL(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	tenant, project, intent, actor, session, child := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	sha, err := exposureSourceRequestSHA(prepared.Sources.RunID, request)
	if err != nil {
		t.Fatal(err)
	}
	scope := PreparedExposureSourceScope{child: child, binding: exposureSourceBinding{tenant, intent, actor, session, prepared.Sources.RunID, sha}}
	if !scope.matches(tenant, intent, actor, session, child, prepared, request) {
		t.Fatal("original namespace refused")
	}
	original := []uuid.UUID{tenant, intent, actor, session, child}
	for i := range original {
		ids := append([]uuid.UUID(nil), original...)
		ids[i] = uuid.New()
		tx := &exposureInputTx{}
		result, err := CreateSourceVerifiedExposureDraftInTransaction(context.Background(), tx, ids[0], project, ids[1], ids[2], ids[3], ids[4], prepared, targets, request, PublishedDecision{}, scope, nil)
		if err == nil || result.Nodes != nil || len(tx.calls) != 0 {
			t.Fatal("foreign scope reached SQL", i)
		}
	}
	changed := prepared
	changed.Sources.RunID = uuid.New()
	if scope.matches(tenant, intent, actor, session, child, changed, request) {
		t.Fatal("changed parent matched")
	}
	changedRequest := request
	changedRequest.TargetDecisions = append([]TargetDecision(nil), request.TargetDecisions...)
	changedRequest.TargetDecisions[0].Decision = "reject"
	if scope.matches(tenant, intent, actor, session, child, prepared, changedRequest) {
		t.Fatal("changed decisions matched")
	}
	tx := &exposureInputTx{}
	if result, err := CreateSourceVerifiedExposureDraftInTransaction(context.Background(), tx, tenant, project, intent, actor, session, child, prepared, targets, request, PublishedDecision{}, PreparedExposureSourceScope{}, nil); err == nil || result.Nodes != nil || len(tx.calls) != 0 {
		t.Fatal("zero scope queried")
	}
}
