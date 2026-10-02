package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestCompositionDraftPreflightRejectsMutationBeforeDatabaseIO(t *testing.T) {
	run, session, project, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	requests := compositionDraftRequests(t, run, session)
	tenant, who := requests[0].Preparation.Tenant, requests[0].Preparation.Actor
	compile := func() CompiledCompositionDraft {
		draft, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests)
		if err != nil {
			t.Fatal(err)
		}
		return draft
	}
	// Uninitialized pool deliberately cannot be queried. Every rejection below
	// must occur before transaction creation, rather than a database fallback.
	store := SQLOriginalCompositionPreflight{DB: &pgxpool.Pool{}}
	changes := []func(*CompiledCompositionDraft){
		func(d *CompiledCompositionDraft) { d.Body = append(d.Body, ' ') },
		func(d *CompiledCompositionDraft) { d.SHA256 = "invalid" },
		func(d *CompiledCompositionDraft) { d.Config.ProjectID = uuid.New() },
		func(d *CompiledCompositionDraft) { d.OutputBytes = 0 },
		func(d *CompiledCompositionDraft) { d.OutputBytes = 9007199254740992 },
		func(d *CompiledCompositionDraft) {
			d.Config.Composition.Nodes[0].ParamsJCS += " "
			d.Body, _ = canonical.Marshal(d.Config)
			d.SHA256, _ = canonical.Hash(d.Config)
		},
	}
	for _, change := range changes {
		draft := compile()
		change(&draft)
		if err := store.CheckDraft(context.Background(), tenant, run, who, draft); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("mutated plan performed IO", err)
		}
	}
	for _, identity := range []auth.Identity{{}, {UserID: who.UserID}, {SessionID: session}} {
		if err := store.CheckDraft(context.Background(), tenant, run, identity, compile()); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := store.CheckDraft(ctx, tenant, run, who, compile()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := store.CheckTaskParams(context.Background(), tenant, project, snapshot, who, uuid.Nil, v2input.Ref{}, nil, 1, 1, nil); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	if err := store.CheckTaskParams(ctx, tenant, project, snapshot, who, uuid.New(), v2input.Ref{}, nil, 1, 1, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
