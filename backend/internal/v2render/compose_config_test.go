package v2render

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func compositionDraftRequests(t *testing.T, run, session uuid.UUID) []CompositionDraftRequest {
	t.Helper()
	workflow, _ := v2input.WorkflowID(run)
	tenant, actor := uuid.New(), uuid.New()
	result := make([]CompositionDraftRequest, 2)
	for i := range result {
		layer, base, proposal := compositionInputs()
		compiled, err := CompileComposition(layer, base, nil, proposal, nil, 5, 5)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(compiled.JSON)
		params := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
		task, _ := v2input.TaskID(workflow, "layer.compose", i)
		roots, err := CompositionInputRoots(compiled, params)
		if err != nil {
			t.Fatal(err)
		}
		result[i] = CompositionDraftRequest{Ordinal: i, Preparation: CompositionPreparation{Tenant: tenant, Task: task, Actor: auth.Identity{UserID: actor, SessionID: session}, Layer: layer, Base: base, ProposalResult: proposal, Width: 5, Height: 5, ParamsRef: params}, Resolved: roots}
	}
	return result
}

func TestCompositionDraftConfigStableAndOwnsPlan(t *testing.T) {
	run, session, project, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	requests := compositionDraftRequests(t, run, session)
	first, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests)
	if err != nil {
		t.Fatal(err)
	}
	requests[0], requests[1] = requests[1], requests[0]
	second, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests)
	if err != nil || string(first.Body) != string(second.Body) || first.SHA256 != second.SHA256 {
		t.Fatal("unstable ordinal order", err)
	}
	hash, err := canonical.Hash(first.Config)
	if err != nil || hash != first.SHA256 || len(first.Tasks) != 2 || len(first.Inputs) != 2 || first.OutputBytes != 2*(24*25+32*1024*1024) {
		t.Fatal("invalid frozen plan", err)
	}
	requests[0].Preparation.Layer.Mask[0] = ' '
	requests[0].Resolved[0].Ref.ArtifactID = uuid.New()
	ownedBody, err := canonical.Marshal(first.Config)
	if err != nil || string(ownedBody) != string(first.Body) {
		t.Fatal("caller mutation changed frozen plan", err)
	}
	node := first.Config.Composition.Nodes[1]
	if node.ParamsJCS == "" || node.Params.ArtifactID != first.Tasks[1].ParamsArtifactID || node.InputDigest != first.Inputs[1].InputDigest {
		t.Fatal("lost document binding")
	}
	first.Tasks[1].ExecutionProfile[0] = ' '
	if first.Config.Composition.Nodes[1].Profile[0] == ' ' {
		t.Fatal("plan aliases task profile")
	}
}

func TestCompositionDraftConfigRejectsScopeAndIdentityChanges(t *testing.T) {
	run, session, project, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	changes := []func([]CompositionDraftRequest){
		func(r []CompositionDraftRequest) { r[1].Ordinal = 0 },
		func(r []CompositionDraftRequest) { r[1].Ordinal = 2 },
		func(r []CompositionDraftRequest) { r[1].Preparation.Tenant = uuid.New() },
		func(r []CompositionDraftRequest) { r[1].Preparation.Actor.UserID = uuid.New() },
		func(r []CompositionDraftRequest) { r[1].Preparation.Actor.SessionID = uuid.New() },
		func(r []CompositionDraftRequest) { r[1].Preparation.Task = uuid.New() },
		func(r []CompositionDraftRequest) {
			r[1].Preparation.ParamsRef.SHA256 = r[1].Preparation.ProposalResult.SHA256
		},
		func(r []CompositionDraftRequest) { r[1].Resolved = nil },
	}
	for _, change := range changes {
		requests := compositionDraftRequests(t, run, session)
		change(requests)
		if _, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests); err == nil {
			t.Fatal("changed identity/scope accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileCompositionDraftConfig(ctx, project, snapshot, session, run, compositionDraftRequests(t, run, session)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCompositionDraftConfigRequiresBoundedSelection(t *testing.T) {
	run, session, project, snapshot := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	requests := compositionDraftRequests(t, run, session)
	for _, ids := range [][4]uuid.UUID{
		{uuid.Nil, snapshot, session, run}, {project, uuid.Nil, session, run},
		{project, snapshot, uuid.Nil, run}, {project, snapshot, session, uuid.Nil},
	} {
		if _, err := CompileCompositionDraftConfig(context.Background(), ids[0], ids[1], ids[2], ids[3], requests); err == nil {
			t.Fatal("nil selection identity accepted")
		}
	}
	for _, selection := range [][]CompositionDraftRequest{nil, make([]CompositionDraftRequest, 5001)} {
		if _, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, selection); err == nil {
			t.Fatal("unbounded/empty selection accepted")
		}
	}
}

func TestPinnedCompositionConfigFreezesRuntime(t *testing.T) {
	run, session := uuid.New(), uuid.New()
	requests := compositionDraftRequests(t, run, session)
	project, snapshot := uuid.New(), uuid.New()
	pin := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	result, err := CompilePinnedCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests, pin)
	if err != nil {
		t.Fatal(err)
	}
	for i, task := range result.Tasks {
		var profile map[string]any
		if err := json.Unmarshal(task.ExecutionProfile, &profile); err != nil {
			t.Fatal(err)
		}
		if profile["layer_runtime_sha256"] != pin {
			t.Fatal("missing pin")
		}
		if string(task.ExecutionProfile) != string(result.Config.Composition.Nodes[i].Profile) {
			t.Fatal("profile mismatch")
		}
	}
	unpinned, err := CompileCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests)
	if err != nil || result.SHA256 == unpinned.SHA256 {
		t.Fatal("pin must change frozen plan hash")
	}
	for _, bad := range []string{"", "Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"} {
		if _, err := CompilePinnedCompositionDraftConfig(context.Background(), project, snapshot, session, run, requests, bad); err == nil {
			t.Fatal("invalid pin admitted")
		}
	}
}

func TestCompositionAlphaPolicyIsFrozen(t *testing.T) {
	run, session := uuid.New(), uuid.New()
	project, snapshot := uuid.New(), uuid.New()
	requests := compositionDraftRequests(t, run, session)
	pin := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	strict, err := CompileCompositionDraftConfigWithAlpha(context.Background(), project, snapshot, session, run, requests, pin, "require_float32")
	if err != nil {
		t.Fatal(err)
	}
	converted, err := CompileCompositionDraftConfigWithAlpha(context.Background(), project, snapshot, session, run, requests, pin, "normalize_uint16")
	if err != nil {
		t.Fatal(err)
	}
	if strict.SHA256 == converted.SHA256 {
		t.Fatal("alpha choice must change plan hash")
	}
	var profile map[string]any
	if err := json.Unmarshal(converted.Tasks[0].ExecutionProfile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["layer_alpha_mode"] != "normalize_uint16" {
		t.Fatal("missing alpha choice")
	}
	var strictProfile map[string]any
	if err := json.Unmarshal(strict.Tasks[0].ExecutionProfile, &strictProfile); err != nil {
		t.Fatal(err)
	}
	before := strictProfile["resource_estimate"].(map[string]any)
	after := profile["resource_estimate"].(map[string]any)
	node := converted.Config.Composition.Nodes[0]
	for _, field := range []string{"ram_bytes", "scratch_bytes"} {
		if after[field].(float64)-before[field].(float64) != float64(8*node.Width*node.Height+8192) {
			t.Fatalf("missing alpha reservation for %s", field)
		}
	}
	if _, err := CompileCompositionDraftConfigWithAlpha(context.Background(), project, snapshot, session, run, requests, pin, "automatic"); err == nil {
		t.Fatal("implicit conversion admitted")
	}
}
