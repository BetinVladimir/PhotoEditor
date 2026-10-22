package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestAuthoredReplayPlannedFirstGeometryPrefix(t *testing.T) {
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	tenant, parent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	source := AuthoredInstructionSource{Tenant: tenant, Parent: parent, Actor: actor, Session: session, ProgramSHA: strings.Repeat("c", 64), Source: binding, OriginalRecipe: ref()}
	source.Instruction.Ordinal = 1
	source.Instruction.Identity = uuid.New()
	source.Instruction.Kind = "geometry"
	base := map[string]any{"tenant_id": tenant, "parent_run_id": parent, "original_recipe": source.OriginalRecipe, "processed_source": binding}
	body, err := canonical.Marshal(base)
	if err != nil {
		t.Fatal(err)
	}
	sha, err := canonical.Hash(base)
	if err != nil {
		t.Fatal(err)
	}
	baseRef := ref()
	baseRef.SHA256 = sha
	source.PredecessorSHA = sha
	inventory := AuthoredFinalInventory{Context: source, BaseReceipt: baseRef, BaseReceiptJCS: string(body), Phases: []AuthoredPublishedPhase{}}
	request := v2input.AuthoredNativeReplayRequest{}
	request.Consumer.Tenant = tenant
	request.Consumer.Parent = parent
	request.Consumer.Ordinal = 1
	request.Consumer.Instruction = source.Instruction.Identity
	request.Consumer.ProgramSHA = source.ProgramSHA
	request.Consumer.PredecessorSHA = sha
	request.Consumer.Source = binding
	request.Consumer.Operation = "render.authored_geometry"
	who := auth.Identity{UserID: actor, SessionID: session}
	raw, _ := canonical.Marshal(inventory)
	got, err := decodeAuthoredReplayPlannedInventory(context.Background(), raw, request, who)
	if err != nil || got.BaseReceipt != baseRef {
		t.Fatalf("first planned geometry: %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*AuthoredFinalInventory)
	}{
		{"actor", func(v *AuthoredFinalInventory) { v.Context.Actor = uuid.New() }},
		{"program", func(v *AuthoredFinalInventory) { v.Context.ProgramSHA = strings.Repeat("d", 64) }},
		{"source", func(v *AuthoredFinalInventory) { v.Context.Source.Source.ArtifactID = uuid.New() }},
		{"base bytes", func(v *AuthoredFinalInventory) { v.BaseReceiptJCS = "{}" }},
		{"future phase", func(v *AuthoredFinalInventory) { v.Phases = []AuthoredPublishedPhase{{Ordinal: 1}} }},
		{"wrong kind", func(v *AuthoredFinalInventory) { v.Context.Instruction.Kind = "finalize" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := inventory
			tc.mutate(&v)
			raw, _ := canonical.Marshal(v)
			if _, err := decodeAuthoredReplayPlannedInventory(context.Background(), raw, request, who); err == nil {
				t.Fatal("foreign planned prefix accepted")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := decodeAuthoredReplayPlannedInventory(ctx, raw, request, who); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}

func TestAuthoredReplaySegmentOccurrenceCannotMoveSource(t *testing.T) {
	tenant, parent := uuid.New(), uuid.New()
	inventory := AuthoredFinalInventory{}
	inventory.Context.Workflow = uuid.New()
	inventory.Context.ProgramSHA = strings.Repeat("a", 64)
	inventory.Context.OriginalRecipe = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	inventory.BaseReceipt = v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	phase := AuthoredPhaseReceipt{Tenant: tenant, Parent: parent, Kind: "segment", Ordinal: 1, Instruction: uuid.New(), PredecessorSHA: inventory.BaseReceipt.SHA256}
	phase.InputSource = v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("d", 64)}, CanvasHash: strings.Repeat("e", 64)}
	p := AuthoredSegmentParams{TenantID: tenant, ParentRunID: parent, WorkflowID: inventory.Context.Workflow, ProgramSHA256: inventory.Context.ProgramSHA, Recipe: inventory.Context.OriginalRecipe, BaseReceipt: inventory.BaseReceipt, Ordinal: 1, InstructionID: phase.Instruction, PredecessorSHA256: phase.PredecessorSHA, Source: phase.InputSource}
	if !authoredReplaySegmentOccurrenceMatches(p, phase, inventory, tenant, parent) {
		t.Fatal("exact prior occurrence refused")
	}
	p.Source.Source.ArtifactID = uuid.New()
	if authoredReplaySegmentOccurrenceMatches(p, phase, inventory, tenant, parent) {
		t.Fatal("foreign source accepted")
	}
	p.Source = phase.InputSource
	p.PhaseReceipts = []v2input.Ref{inventory.BaseReceipt}
	if authoredReplaySegmentOccurrenceMatches(p, phase, inventory, tenant, parent) {
		t.Fatal("future receipt accepted")
	}
}
