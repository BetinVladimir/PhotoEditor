package semanticreview

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type protectionQuery struct {
	pin   v2input.Ref
	err   error
	calls int
	args  []any
}
type protectionRow struct{ query *protectionQuery }

func (r protectionRow) Scan(dest ...any) error {
	if r.query.err != nil {
		return r.query.err
	}
	*dest[0].(*uuid.UUID) = r.query.pin.ArtifactID
	*dest[1].(*string) = r.query.pin.SHA256
	return nil
}
func (q *protectionQuery) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.calls++
	q.args = args
	return protectionRow{q}
}
func TestProtectionDiscoveryBindsPrivateReviewBeforeSQL(t *testing.T) {
	prepared, targets, request := decisionProofKindFixture(t, "local_exposure")
	var selected LockedTarget
	for _, target := range targets {
		if target.ID == request.TargetDecisions[0].TargetID {
			selected = target
		}
	}
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	q := &protectionQuery{pin: pin}
	invoke := func(ctx context.Context, req ApplyDecision) (map[uuid.UUID]v2input.Ref, error) {
		return ReadApprovedProtectionPins(ctx, q, tenant, intent, actor, session, prepared, targets, req)
	}
	found, err := invoke(context.Background(), request)
	if err != nil || found[selected.ID] != pin || q.calls != 1 {
		t.Fatal("pin discovery", err)
	}
	if q.args[0] != tenant || q.args[1] != intent || q.args[2] != actor || q.args[3] != session || q.args[5] != selected.ID || q.args[7] != selected.AssetVersionID || q.args[8] != *selected.CandidateArtifactID {
		t.Fatal("scope changed")
	}
	bad := request
	bad.PlanSHA = strings.Repeat("0", 64)
	if found, err := invoke(context.Background(), bad); err == nil || found != nil || q.calls != 1 {
		t.Fatal("changed review reached SQL")
	}
	q.err = pgx.ErrNoRows
	if found, err := invoke(context.Background(), request); !errors.Is(err, pgx.ErrNoRows) || found != nil {
		t.Fatal("missing pin accepted")
	}
	q.err = nil
	q.pin.SHA256 = "bad"
	if found, err := invoke(context.Background(), request); err == nil || found != nil {
		t.Fatal("invalid pin accepted")
	}
	before := q.calls
	reject := request
	reject.TargetDecisions = append([]TargetDecision(nil), request.TargetDecisions...)
	reject.TargetDecisions[0].Decision = "reject"
	if found, err := invoke(context.Background(), reject); err != nil || len(found) != 0 || q.calls != before {
		t.Fatal("rejected target read pin", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if found, err := invoke(ctx, request); !errors.Is(err, context.Canceled) || found != nil || q.calls != before {
		t.Fatal("cancel read pin")
	}
}
