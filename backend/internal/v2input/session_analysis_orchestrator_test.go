package v2input

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

type stableAnalysisDocuments struct {
	scopes []artifactjson.Scope
	refs   map[string]artifactjson.Ref
}

type analysisImageSource struct {
	inputs []SessionDescribeImageInputs
	docs   *stableAnalysisDocuments
}

type analysisConsentPinner struct{}

func (analysisConsentPinner) Pin(_ context.Context, _, _ uuid.UUID,
	snapshot SessionInputSnapshot) ([]byte, error) {
	if snapshot.SchemaVersion != 1 || len(snapshot.Items) != 1 {
		return nil, ErrInvalid
	}
	return []byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`), nil
}

func (s analysisImageSource) Load(_ context.Context, _, _ uuid.UUID,
	items []SessionSnapshotItem) ([]SessionDescribeImageInputs, error) {
	if len(s.docs.scopes)%4 != 0 || len(items) != len(s.inputs) {
		return nil, ErrInvalid
	}
	return s.inputs, nil
}

func (d *stableAnalysisDocuments) Publish(_ context.Context,
	scope artifactjson.Scope, body []byte) (artifactjson.Ref, error) {
	d.scopes = append(d.scopes, scope)
	sha, err := canonical.Hash(json.RawMessage(body))
	if err != nil {
		return artifactjson.Ref{}, err
	}
	key := scope.Run.String() + ":" + scope.Owner.String() + ":" + scope.Purpose
	if prior, ok := d.refs[key]; ok {
		if prior.SHA256 != sha {
			return artifactjson.Ref{}, ErrConflict
		}
		return prior, nil
	}
	ref := artifactjson.Ref{ID: uuid.NewSHA1(scope.Run, []byte(key)), SHA256: sha}
	d.refs[key] = ref
	return ref, nil
}

type analysisStageRecorder struct {
	docs      *stableAnalysisDocuments
	transport *memoryPublisher
	count     int
	staged    bool
}

func (r *analysisStageRecorder) Stage(_ context.Context, _, _, _ uuid.UUID,
	plan SessionDescriptorPlan, tasks []AnalysisDescribeStageTask) error {
	if len(r.docs.scopes) != (r.count+1)*4 ||
		len(r.transport.bodies) != 2*r.count ||
		len(plan.Tasks) != 1 || len(tasks) != 1 ||
		!validRef(tasks[0].Params) || !digestPattern.MatchString(tasks[0].InputDigest) {
		return ErrInvalid
	}
	r.count++
	r.staged = true
	return nil
}

type analysisCommitRecorder struct {
	stage *analysisStageRecorder
	graph []GraphTask
	err   error
}

func (r *analysisCommitRecorder) CommitGraph(_ context.Context, _, _ uuid.UUID,
	graph []GraphTask) error {
	if !r.stage.staged || len(graph) != 1 || graph[0].Built.Count != 7 ||
		len(graph[0].Direct) != 4 {
		return ErrInvalid
	}
	r.graph = graph
	return r.err
}

func TestSessionAnalysisOrchestratorStagesBeforeTransportAndCommits(t *testing.T) {
	run, tenant := uuid.New(), uuid.New()
	workflow, _ := WorkflowID(run)
	image := Ref{uuid.New(), strings.Repeat("a", 64)}
	recipe := Ref{uuid.New(), strings.Repeat("b", 64)}
	pixels := Ref{uuid.New(), strings.Repeat("c", 64)}
	calibration := Ref{uuid.New(), strings.Repeat("d", 64)}
	items := []SessionSnapshotItem{{Ordinal: 0,
		Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: image, CanvasHash: strings.Repeat("e", 64)},
		WorkingImage: image, Recipe: recipe}}
	snapshot, _, err := CompileSessionInputSnapshot(run, items)
	if err != nil {
		t.Fatal(err)
	}
	contextBody, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "snapshot_id": snapshot.SnapshotID,
		"snapshot_digest":   snapshot.SnapshotDigest,
		"manual_references": []any{}, "manual_locked_ordinals": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	contextSHA, _ := canonical.Hash(json.RawMessage(contextBody))
	docs := &stableAnalysisDocuments{refs: make(map[string]artifactjson.Ref)}
	transport := &memoryPublisher{bodies: make(map[uuid.UUID][]byte)}
	stage := &analysisStageRecorder{docs: docs, transport: transport}
	commit := &analysisCommitRecorder{stage: stage}
	orchestrator := SessionAnalysisOrchestrator{
		Documents: docs, Transport: transport, Stage: stage, Committer: commit,
		Consents: analysisConsentPinner{},
		Images:   analysisImageSource{inputs: []SessionDescribeImageInputs{{Ordinal: 0, Pixels: pixels}}, docs: docs}}
	draft := SessionAnalysisDraft{Tenant: tenant, Run: run, Workflow: workflow,
		Items:          items,
		SnapshotDigest: snapshot.SnapshotDigest, ContextBody: contextBody,
		ContextSHA:  contextSHA,
		Calibration: calibration, ModelLockSHA: strings.Repeat("f", 64),
		PolicySHA: strings.Repeat("1", 64), Profiles: []json.RawMessage{
			json.RawMessage(`{"backend":"cuda","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":6442450944,"scratch_bytes":1073741824,"output_bytes":8388608,"pixel_count":0}}`)}}
	ticket, err := orchestrator.AdmitPending(context.Background(), draft)
	if err != nil || ticket.RunID != run || ticket.WorkflowID != workflow ||
		ticket.State != "queued" || stage.count != 1 || len(commit.graph) != 1 ||
		len(transport.bodies) != 2 {
		t.Fatalf("admission: %+v %v stage=%d graph=%d transport=%d",
			ticket, err, stage.count, len(commit.graph), len(transport.bodies))
	}
	commit.err = errors.New("commit failed")
	if _, err := orchestrator.AdmitPending(context.Background(), draft); err == nil {
		t.Fatal("accepted failed graph commit")
	}
	bad := draft
	bad.SnapshotDigest = strings.Repeat("0", 64)
	before := len(docs.scopes)
	if _, err := orchestrator.AdmitPending(context.Background(), bad); err == nil ||
		len(docs.scopes) != before {
		t.Fatal("published changed frozen snapshot")
	}
}
