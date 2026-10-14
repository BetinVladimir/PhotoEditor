package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"sort"
	"strings"
	"testing"
)

func TestPreparedSourcePlanRejectsBeforeSQL(t *testing.T) {
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	request := Request{TenantID: uuid.New(), ActorID: who.UserID, RunID: uuid.New()}
	plan := WorkflowPlan{tenant: request.TenantID, actor: who.UserID, run: request.RunID, project: uuid.New(), sourcesRequired: true}
	for _, change := range []func(*WorkflowPlan){
		func(p *WorkflowPlan) {},
		func(p *WorkflowPlan) { p.tenant = uuid.New() },
		func(p *WorkflowPlan) { p.actor = uuid.New() },
		func(p *WorkflowPlan) { p.project = uuid.Nil },
		func(p *WorkflowPlan) { p.sources = []sourcePreparationProof{{}} },
		func(p *WorkflowPlan) {
			p.inventory.Targets = []v2input.SessionVersionBinding{{}}
			p.sources = []sourcePreparationProof{{}, {}}
		},
	} {
		changed := plan
		change(&changed)
		tx := &manualLockTx{role: "photo_scheduler"}
		if err := LockPreparedSourcesInTransaction(context.Background(), tx, who, request, changed); err != v2input.ErrInvalid || len(tx.calls) != 0 {
			t.Fatal("invalid source plan reached SQL", err, tx.calls)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tx := &manualLockTx{role: "photo_scheduler"}
	if err := LockPreparedSourcesInTransaction(ctx, tx, who, request, plan); !errors.Is(err, context.Canceled) || len(tx.calls) != 0 {
		t.Fatal("cancelled source plan reached SQL", err)
	}
}

// Records transaction tenant setup as well as source lock queries.
type sourcePlanTx struct{ manualLockTx }

func (t *sourcePlanTx) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.calls = append(t.calls, sql)
	t.args = append(t.args, args)
	if len(t.calls) == t.failAt {
		return pgconn.CommandTag{}, errors.New("injected SQL failure")
	}
	return pgconn.CommandTag{}, nil
}
func TestPreparedSourcesDeduplicateAndStopEverySQLFailure(t *testing.T) {
	m := manualPinTestProof()
	root := m.objects.Root.object
	root.SHA256 = m.binding.Source.SHA256
	pins := []sourcePreparationPin{{v2draft.LayerRef{Ref: m.binding.Source, Contract: "WorkingImage", CanvasHash: m.binding.CanvasHash}, root}, {v2draft.LayerRef{Ref: m.objects.Pixel.ref, Contract: "WorkingPixels", ParentRef: m.binding.Source}, m.objects.Pixel.object}}
	body, _ := canonical.Marshal(pins)
	reference := sourcePreparationProof{tenant: m.tenant, project: m.project, actor: m.actor, session: m.session, bucket: "bucket", binding: m.binding, iccSHA: m.binding.Source.SHA256, pins: string(body)}
	target := reference
	target.binding.AssetVersionID = uuid.New()
	who := auth.Identity{UserID: m.actor, SessionID: m.session}
	request := Request{TenantID: m.tenant, ActorID: m.actor, RunID: uuid.New(), Intent: v2input.SemanticEditIntent{Reference: m.binding}}
	plan := WorkflowPlan{tenant: m.tenant, actor: m.actor, run: request.RunID, project: m.project, sourcesRequired: true, inventory: CatalogInventory{Targets: []v2input.SessionVersionBinding{target.binding, target.binding}}, sources: []sourcePreparationProof{reference, target, reference, target}}
	tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := LockPreparedSourcesInTransaction(context.Background(), tx, who, request, plan); err != nil {
		t.Fatal(err)
	}
	if len(tx.calls) != 6 || !strings.Contains(tx.calls[1], "set_config") || !strings.Contains(tx.calls[3], "source_evidence") || !strings.Contains(tx.calls[5], "source_evidence") {
		t.Fatal("source lock/dedup sequence", tx.calls)
	}
	expected := []uuid.UUID{reference.binding.AssetVersionID, target.binding.AssetVersionID}
	sort.Slice(expected, func(i, j int) bool { return expected[i].String() < expected[j].String() })
	if tx.args[3][4] != expected[0] || tx.args[5][4] != expected[1] {
		t.Fatal("source order differs", tx.args)
	}
	for fail := 1; fail <= 6; fail++ {
		tx := &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler", failAt: fail, err: errors.New("query failure")}}
		if err := LockPreparedSourcesInTransaction(context.Background(), tx, who, request, plan); err == nil || len(tx.calls) != fail {
			t.Fatal("source failure continued", fail, err, len(tx.calls))
		}
	}
	changed := plan
	changed.sources = append([]sourcePreparationProof(nil), plan.sources...)
	changed.sources[2].bucket = "different"
	tx = &sourcePlanTx{manualLockTx: manualLockTx{role: "photo_scheduler"}}
	if err := LockPreparedSourcesInTransaction(context.Background(), tx, who, request, changed); err != v2input.ErrConflict || len(tx.calls) != 0 {
		t.Fatal("conflicting duplicate reached SQL", err)
	}
}
