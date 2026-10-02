package v2input

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
)

type analysisStageRow bool

func (r analysisStageRow) Scan(dest ...any) error {
	*dest[0].(*bool) = bool(r)
	return nil
}

type analysisStageDB struct {
	calls int
	rows  []json.RawMessage
}

func (d *analysisStageDB) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	d.calls++
	if !strings.Contains(query, "stage_v22_analysis_describes") || len(args) != 3 {
		return analysisStageRow(false)
	}
	_ = json.Unmarshal(args[2].(json.RawMessage), &d.rows)
	return analysisStageRow(true)
}

func TestPublishSessionDescribeParamsAndStage(t *testing.T) {
	workflow, tenant, run := uuid.New(), uuid.New(), uuid.New()
	plan, err := CompileSessionDescriptors(workflow, 129)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := Ref{uuid.New(), strings.Repeat("a", 64)}
	calibration := Ref{uuid.New(), strings.Repeat("b", 64)}
	consents := Ref{uuid.New(), strings.Repeat("c", 64)}
	docs := &scopedDocuments{}
	staged := make([]AnalysisDescribeStageTask, len(plan.Tasks))
	for i, task := range plan.Tasks {
		ref, err := PublishSessionDescribeParams(context.Background(), docs,
			tenant, run, i, task, snapshot, calibration, consents,
			strings.Repeat("d", 64))
		if err != nil {
			t.Fatal(err)
		}
		if docs.scopes[i].Tenant != tenant || docs.scopes[i].Run != run ||
			docs.scopes[i].Owner != task.ID ||
			docs.scopes[i].Purpose != "v2_analysis_describe_params_"+string(rune('0'+i)) {
			t.Fatal("incorrect shard scope", docs.scopes[i])
		}
		staged[i] = AnalysisDescribeStageTask{Params: ref,
			InputDigest: strings.Repeat("e", 64),
			Profile:     json.RawMessage(`{"backend":"cuda","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":6442450944,"scratch_bytes":1073741824,"output_bytes":8388608,"pixel_count":0}}`)}
	}
	db := &analysisStageDB{}
	store := SQLAnalysisStageStore{DB: db}
	if err := store.Stage(context.Background(), tenant, run, workflow, plan, staged); err != nil ||
		db.calls != 1 || len(db.rows) != 2 {
		t.Fatalf("stage: %v, calls=%d, rows=%d", err, db.calls, len(db.rows))
	}
	staged[0].InputDigest = "broken"
	if err := store.Stage(context.Background(), tenant, run, workflow, plan, staged); err == nil || db.calls != 1 {
		t.Fatal("invalid digest reached SQL")
	}
	plan.Tasks[1].Ordinals[0] = 5
	staged[0].InputDigest = strings.Repeat("e", 64)
	if err := store.Stage(context.Background(), tenant, run, workflow, plan, staged); err == nil || db.calls != 1 {
		t.Fatal("invalid shard coverage reached SQL")
	}
	plan, _ = CompileSessionDescriptors(workflow, 129)
	staged[0].Profile = json.RawMessage(`{"backend":"cuda","renderer_version":"v2.2"}`)
	if err := store.Stage(context.Background(), tenant, run, workflow, plan, staged); err == nil || db.calls != 1 {
		t.Fatal("missing resource estimate reached SQL")
	}
}

func TestPublishSessionAnalysisRootsPinsFrozenDigests(t *testing.T) {
	tenant, run := uuid.New(), uuid.New()
	image := Ref{uuid.New(), strings.Repeat("a", 64)}
	recipe := Ref{uuid.New(), strings.Repeat("b", 64)}
	items := []SessionSnapshotItem{{Ordinal: 0,
		Binding: SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: image, CanvasHash: strings.Repeat("c", 64)},
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
	contextSHA, err := canonical.Hash(json.RawMessage(contextBody))
	if err != nil {
		t.Fatal(err)
	}
	docs := &scopedDocuments{}
	refs, err := PublishSessionAnalysisRoots(context.Background(), docs, tenant,
		run, items, snapshot.SnapshotDigest, contextBody, contextSHA,
		[]byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`))
	if err != nil || refs.SnapshotDigest != snapshot.SnapshotDigest ||
		refs.Context.SHA256 != contextSHA || len(docs.scopes) != 3 {
		t.Fatalf("root publication: %+v %v scopes=%d snapshot=%s context=%s", refs, err, len(docs.scopes), snapshot.SnapshotDigest, contextSHA)
	}
	for i, purpose := range []string{"v2_analysis_input_snapshot",
		"v2_analysis_context", "v2_analysis_consents"} {
		if docs.scopes[i].Purpose != purpose || docs.scopes[i].Tenant != tenant ||
			docs.scopes[i].Run != run || docs.scopes[i].Owner != run {
			t.Fatalf("invalid root scope: %+v", docs.scopes[i])
		}
	}
	if _, err := PublishSessionAnalysisRoots(context.Background(), docs, tenant,
		run, items, strings.Repeat("d", 64), contextBody, contextSHA,
		[]byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`)); err == nil || len(docs.scopes) != 3 {
		t.Fatal("published changed snapshot digest")
	}
	brokenContext := []byte(`{"manual_locked_ordinals":[],"manual_references":[],"schema_version":1}`)
	brokenSHA, _ := canonical.Hash(json.RawMessage(brokenContext))
	if _, err := PublishSessionAnalysisRoots(context.Background(), docs, tenant,
		run, items, snapshot.SnapshotDigest, brokenContext, brokenSHA,
		[]byte(`{"confirmed_faces":[],"grants":[],"schema_version":1}`)); err == nil || len(docs.scopes) != 3 {
		t.Fatal("published incomplete plan context")
	}
}

func TestSessionConsentSnapshotPinsFaceGrant(t *testing.T) {
	person, consent, asset, face := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	valid := map[string]any{"schema_version": 1,
		"grants": []any{map[string]any{"consent_id": consent,
			"epoch": "2", "person_id": person, "purpose": "identity_matching"}},
		"confirmed_faces": []any{map[string]any{"asset_version_id": asset,
			"face_id": face, "person_id": person, "consent_id": consent, "epoch": "2"}}}
	body, _ := json.Marshal(valid)
	if err := validateSessionConsentSnapshot(body); err != nil {
		t.Fatal(err)
	}
	bad := map[string]any{"schema_version": 1, "grants": valid["grants"],
		"confirmed_faces": []any{map[string]any{"asset_version_id": asset,
			"face_id": face, "person_id": person, "consent_id": consent, "epoch": "3"}}}
	body, _ = json.Marshal(bad)
	if err := validateSessionConsentSnapshot(body); err == nil {
		t.Fatal("accepted stale consent epoch")
	}
	bad["confirmed_faces"] = []any{}
	bad["grants"] = []any{map[string]any{"consent_id": consent,
		"epoch": "02", "person_id": person, "purpose": "identity_matching"}}
	body, _ = json.Marshal(bad)
	if err := validateSessionConsentSnapshot(body); err == nil {
		t.Fatal("accepted noncanonical epoch")
	}
}
