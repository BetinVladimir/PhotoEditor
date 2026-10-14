package v2region

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestPrepareRegionNodeRejectsBeforePublicationAndBindsPublishedRef(t *testing.T) {
	plan, task, _, profile := regionNodeFixture(t)
	raw, _ := json.Marshal(profile)
	resolver := &regionInventoryResolver{entries: plan.InputRequirements()}
	publisher := &regionParamsFixture{digest: plan.SHA256()}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	if _, err := plan.PrepareNode(context.Background(), who, task, resolver, publisher, []byte(`{"backend":"cpu"}`)); err == nil || resolver.tenant != uuid.Nil || publisher.document != uuid.Nil {
		t.Fatal("invalid runtime reached providers")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := plan.PrepareNode(ctx, who, task, resolver, publisher, raw); err != context.Canceled || publisher.document != uuid.Nil {
		t.Fatal("cancelled preparation published", err)
	}
	resolver.entries = nil
	for _, entry := range plan.InputRequirements() {
		if entry.Contract != "WorkingImage" {
			resolver.entries = append(resolver.entries, entry)
		}
	}
	if _, err := plan.PrepareNode(context.Background(), who, task, resolver, publisher, raw); err == nil || publisher.document != uuid.Nil {
		t.Fatal("incomplete inventory published")
	}
	resolver.entries = plan.InputRequirements()
	node, err := plan.PrepareNode(context.Background(), who, task, resolver, publisher, raw)
	if err != nil {
		t.Fatal(err)
	}
	var document regionNodeDocument
	if json.Unmarshal(node.JSON(), &document) != nil || document.Params.SHA256 != plan.SHA256() || document.Params.ArtifactID == uuid.Nil || document.TaskID != task {
		t.Fatal("published ref not bound")
	}
}

func regionNodeFixture(t *testing.T) (FrozenRegionPlan, uuid.UUID, v2input.Ref, map[string]any) {
	t.Helper()
	req, recipe, sourceRef, source := regionEvidenceFixture(t)
	evidence, err := CompileRegionEvidence(req, recipe, sourceRef, source)
	if err != nil {
		t.Fatal(err)
	}
	project := uuid.New()
	pin := func(ref v2input.Ref, key string, body []byte) RegionSourceMetadata {
		return RegionSourceMetadata{ProjectID: project, Ref: ref, Object: objectstore.Object{Key: key, VersionID: "pinned-v1", Bytes: int64(len(body))}}
	}
	plan, err := freezeRegionPlan(uuid.New(), uuid.New(), project, "private", req,
		pin(sourceRef, "source", source), pin(v2input.Ref{ArtifactID: req.Recipe.ArtifactID, SHA256: req.Recipe.SHA256}, "recipe", recipe), evidence)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := CompileRegionExecutionPolicy(plan, regionCostFixture(t, plan))
	if err != nil {
		t.Fatal(err)
	}
	var profile map[string]any
	if json.Unmarshal(policy.Profile, &profile) != nil {
		t.Fatal("invalid fixture profile")
	}
	profile["max_canvas_pixels"] = 200000000
	return plan, uuid.New(), v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.SHA256()}, profile
}

func TestRegionNodeFreezesExactCommitEvidence(t *testing.T) {
	plan, task, params, profile := regionNodeFixture(t)
	raw, _ := json.Marshal(profile)
	node, err := plan.CompileNode(task, params, plan.InputRequirements(), raw)
	if err != nil {
		t.Fatal(err)
	}
	var document regionNodeDocument
	if json.Unmarshal(node.JSON(), &document) != nil || document.TaskID != task || document.Params != params || document.ParamsJCS != string(plan.JSON()) {
		t.Fatal("node lost exact task/Params binding")
	}
	transport, err := plan.FreezeInputs(task, params, plan.InputRequirements())
	if err != nil || document.InputDigest != transport.InputDigest {
		t.Fatal("wrong input digest", err)
	}
	expectedProfile, _ := canonical.Bytes(raw)
	if !bytes.Equal(document.Profile, expectedProfile) {
		t.Fatal("resource pins were changed")
	}
	copyBody := node.JSON()
	copyBody[0] = '!'
	copyInputs := node.Inputs()
	copyInputs.Direct[0] = v2input.Ref{}
	if !json.Valid(node.JSON()) || node.Inputs().Direct[0] != params {
		t.Fatal("mutable frozen node")
	}
	session := uuid.New()
	config, err := node.ConfigJSON(session)
	var parsed struct {
		Session uuid.UUID            `json:"auth_session_id"`
		Nodes   []regionNodeDocument `json:"nodes"`
	}
	if err != nil || json.Unmarshal(config, &parsed) != nil || parsed.Session != session || len(parsed.Nodes) != 1 || parsed.Nodes[0].TaskID != task {
		t.Fatal("wrong frozen config", err)
	}
	if _, err := node.ConfigJSON(uuid.Nil); err == nil {
		t.Fatal("missing session accepted")
	}
}

func TestRegionNodeRejectsUnboundRuntimeAndTransportDecoration(t *testing.T) {
	for _, key := range []string{"renderer_version", "backend", "icc_lock_sha", "model_lock_sha", "region_runtime_sha256", "contracts_sha", "v2_input_manifest", "v2_entry_count", "v2_entries_sha"} {
		t.Run(key, func(t *testing.T) {
			plan, task, params, profile := regionNodeFixture(t)
			profile[key] = "invalid"
			raw, _ := json.Marshal(profile)
			if _, err := plan.CompileNode(task, params, plan.InputRequirements(), raw); err == nil {
				t.Fatal("unbound profile accepted")
			}
		})
	}
	plan, task, params, profile := regionNodeFixture(t)
	raw, _ := json.Marshal(profile)
	duplicate := append([]byte(`{"backend":"cuda",`), raw[1:]...)
	if _, err := plan.CompileNode(task, params, plan.InputRequirements(), duplicate); err == nil {
		t.Fatal("duplicate profile keys accepted")
	}
	plan.body = append(plan.body, ' ')
	if _, err := plan.CompileNode(task, params, plan.InputRequirements(), raw); err == nil {
		t.Fatal("changed Params bytes accepted")
	}
	if _, err := (FrozenRegionNode{}).ConfigJSON(uuid.New()); err == nil {
		t.Fatal("empty node accepted")
	}
}

func TestRegionShellUsesStableAdmissionIdentities(t *testing.T) {
	plan, _, params, profile := regionNodeFixture(t)
	var document regionPlanDocument
	_ = json.Unmarshal(plan.JSON(), &document)
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	key := "region-shell-test"
	run, _ := v2input.RunID(document.TenantID, who.UserID, key)
	workflow, _ := v2input.WorkflowID(run)
	task, _ := v2input.TaskID(workflow, "region.preview", 0)
	raw, _ := json.Marshal(profile)
	node, err := plan.CompileNode(task, params, plan.InputRequirements(), raw)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := uuid.New()
	shell, sources, err := node.DraftShell(who, key, snapshot, regionSHA, 1024)
	if err != nil || shell.Run != run || shell.WorkflowID != workflow || shell.Snapshot != snapshot || shell.Project != document.ProjectID || shell.Workflow != "preview" || len(shell.Tasks) != 1 || len(sources) != 1 || sources[0].ID != task || shell.Tasks[0].ParamsArtifactID != params.ArtifactID {
		t.Fatal("incorrect shell", err)
	}
	digest, _ := canonical.Hash(json.RawMessage(shell.Config))
	if digest != shell.ConfigSHA {
		t.Fatal("config hash mismatch")
	}
	if _, _, err := node.DraftShell(who, "another-key", snapshot, regionSHA, 1024); err == nil {
		t.Fatal("node moved to another run")
	}
	sources[0].Direct[0] = v2input.Ref{}
	shell.Tasks[0].ExecutionProfile[0] = '!'
	if node.Inputs().Direct[0] != params || !json.Valid(node.JSON()) {
		t.Fatal("shell mutated node")
	}
}

func TestChangedPlanBytesCannotReachResolverOrPublisher(t *testing.T) {
	plan, task, _, _ := regionNodeFixture(t)
	plan.body = append(plan.body, ' ')
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	resolver := &regionInventoryResolver{entries: plan.InputRequirements()}
	publisher := &regionParamsFixture{digest: plan.SHA256()}
	if _, err := plan.ResolveInputs(context.Background(), resolver, who); err == nil || resolver.tenant != uuid.Nil {
		t.Fatal("changed bytes reached resolver")
	}
	if _, err := plan.PublishParams(context.Background(), who, task, publisher); err == nil || publisher.document != uuid.Nil {
		t.Fatal("changed bytes reached publisher")
	}
}

func TestRegionSourceSnapshotPinsRunAndSource(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	run := uuid.New()
	snapshot, err := plan.SourceSnapshot(run)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := plan.SourceSnapshot(run)
	if err != nil || replay.ID != snapshot.ID || replay.SHA256 != snapshot.SHA256 || !bytes.Equal(replay.JSON(), snapshot.JSON()) {
		t.Fatal("unstable snapshot", err)
	}
	var document regionPlanDocument
	_ = json.Unmarshal(plan.JSON(), &document)
	if snapshot.Binding() != document.FrozenRecipe.Source || snapshot.ID != uuid.NewSHA1(run, []byte("region.snapshot")) {
		t.Fatal("snapshot source mismatch")
	}
	copyBody := snapshot.JSON()
	copyBody[0] = '!'
	if !json.Valid(snapshot.JSON()) {
		t.Fatal("mutable snapshot descriptor")
	}
	other, err := plan.SourceSnapshot(uuid.New())
	if err != nil || other.ID == snapshot.ID || other.SHA256 == snapshot.SHA256 {
		t.Fatal("cross-run snapshot reused")
	}
	if _, err := plan.SourceSnapshot(uuid.Nil); err == nil {
		t.Fatal("missing run accepted")
	}
	plan.body = append(plan.body, ' ')
	if _, err := plan.SourceSnapshot(run); err == nil {
		t.Fatal("changed plan snapshot accepted")
	}
}

func TestRegionProfileSourceShapeUsesVerifiedFullCanvas(t *testing.T) {
	plan, _, _, _ := regionNodeFixture(t)
	shape, err := plan.SourceShape()
	if err != nil || shape.Width != plan.canvas.Width || shape.Height != plan.canvas.Height || shape.Width < 1 || shape.Height < 1 {
		t.Fatal("verified full canvas", shape, err)
	}
	shape.Width = 1
	unchanged, err := plan.SourceShape()
	if err != nil || unchanged.Width != plan.canvas.Width {
		t.Fatal("mutable source shape", err)
	}
	changed := plan
	changed.canvas.Width++
	if _, err = changed.SourceShape(); err == nil {
		t.Fatal("substituted canvas accepted")
	}
	changed = plan
	changed.body = append(changed.JSON(), ' ')
	if _, err = changed.SourceShape(); err == nil {
		t.Fatal("modified plan accepted")
	}
	missing := FrozenRegionPlan{body: plan.JSON(), digest: plan.SHA256(), inputs: plan.InputRequirements()}
	if _, err = missing.SourceShape(); err == nil {
		t.Fatal("missing verified source evidence accepted")
	}
}

type cancellingRegionResolver struct{ cancel context.CancelFunc }

func (r cancellingRegionResolver) ResolveRegionInputs(context.Context, auth.Identity, uuid.UUID, v2input.SessionVersionBinding, v2input.Ref, v2draft.CorrectionVector) ([]v2input.Entry, error) {
	r.cancel()
	return nil, nil
}
func TestRegionResolverCancellationDoesNotBecomeInvalidInventory(t *testing.T) {
	plan, task, _, profile := regionNodeFixture(t)
	raw, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	publisher := &regionParamsFixture{digest: plan.SHA256()}
	node, err := plan.PrepareNode(ctx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, task, cancellingRegionResolver{cancel}, publisher, raw)
	if err != context.Canceled || publisher.document != uuid.Nil || len(node.JSON()) != 0 {
		t.Fatal("cancelled resolver advanced or returned validation failure", err)
	}
}
