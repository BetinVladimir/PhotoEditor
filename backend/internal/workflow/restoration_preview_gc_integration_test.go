package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Runs against an isolated database with all forward migrations applied.
func TestRestorationPreviewDraftParamsRetainedBeforeTaskInputs(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"d.v28_restoration_preview_params IS NOT NULL", "d.state='ready'", "r.kind='preview'", "r.status='draft'", "r.v28_restoration_preview_task_id IS NOT NULL", "photo.v2_task_inputs"} {
		if !strings.Contains(definition, required) {
			t.Fatal("preview Params GC reference missing", required)
		}
	}
}

func TestV29NegativeGCMatcherRefusesSubstitutionAndMalformedDraft(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, owner, artifact := "00000000-0000-4000-8000-000000000001", "00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"
	params := "{}"
	hash := sha256.Sum256([]byte(params))
	sha := hex.EncodeToString(hash[:])
	descriptor, err := json.Marshal(map[string]any{"params_jcs": params, "params_ref": map[string]string{"artifact_id": artifact, "sha256": sha}})
	if err != nil {
		t.Fatal(err)
	}
	recovery, err := json.Marshal(map[string]string{"descriptor_jcs": string(descriptor)})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := json.Marshal(map[string]any{"owner": map[string]string{"run_id": run, "task_id": owner}, "params_document_id": artifact, "params_sha": sha, "recovery_jcs": string(recovery)})
	if err != nil {
		t.Fatal(err)
	}
	check := func(raw string, task, digest string, size int64) bool {
		t.Helper()
		var retained bool
		if err := db.QueryRow(ctx, `SELECT photo.v29_negative_gc_draft_match($1,$2::uuid,$3::uuid,$4::uuid,$5,$6)`, raw, run, task, artifact, digest, size).Scan(&retained); err != nil {
			t.Fatal(err)
		}
		return retained
	}
	if !check(string(draft), owner, sha, int64(len(params))) ||
		check(string(draft), run, sha, int64(len(params))) ||
		check(string(draft), owner, strings.Repeat("b", 64), int64(len(params))) ||
		check(string(draft), owner, sha, 3) ||
		check(`{"recovery_jcs":"{"}`, owner, sha, int64(len(params))) {
		t.Fatal("negative draft GC match accepted a substituted or malformed reference")
	}
	var definition string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil || !strings.Contains(definition, "photo.v29_negative_gc_draft_match") {
		t.Fatal("negative draft GC matcher is not installed", err)
	}
}

func TestV29ReplacementGCMatcherCoversSavedDraftForms(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	run, owner, artifact := uuid.New(), uuid.New(), uuid.New()
	params := "{}"
	hash := sha256.Sum256([]byte(params))
	sha := hex.EncodeToString(hash[:])
	marshal := func(value any) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	identity := map[string]string{"run_id": run.String(), "task_id": owner.String(), "params_document_id": artifact.String(), "params_sha": sha}
	direct := marshal(map[string]any{"identity": identity, "draft": map[string]string{
		"run_id": run.String(), "task_id": owner.String(), "params_document_id": artifact.String(), "params_sha": sha, "params_jcs": params,
	}})
	nested := marshal(map[string]any{"identity": identity, "owner": map[string]string{"run_id": run.String(), "task_id": owner.String()},
		"recovery_jcs": marshal(map[string]string{"descriptor_jcs": marshal(map[string]any{
			"params_jcs": params, "params_ref": map[string]string{"artifact_id": artifact.String(), "sha256": sha},
		})})})
	detectorArtifact := uuid.NewSHA1(owner, []byte("params"))
	detector := marshal(map[string]string{"run_id": run.String(), "task_id": owner.String(), "params_jcs": params})
	check := func(saved string, task, ref uuid.UUID, digest string, size int64) bool {
		t.Helper()
		var matched bool
		if err := db.QueryRow(ctx, `SELECT photo.v29_replacement_gc_draft_match($1,$2,$3,$4,$5,$6)`, saved, run, task, ref, digest, size).Scan(&matched); err != nil {
			t.Fatal(err)
		}
		return matched
	}
	if !check(direct, owner, artifact, sha, 2) || !check(nested, owner, artifact, sha, 2) ||
		!check(detector, owner, detectorArtifact, sha, 2) ||
		check(direct, run, artifact, sha, 2) || check(nested, owner, detectorArtifact, sha, 2) ||
		check(detector, owner, artifact, sha, 2) || check(direct, owner, artifact, sha, 3) ||
		check(`{"identity":`, owner, artifact, sha, 2) {
		t.Fatal("replacement GC matcher accepted a substituted or malformed Params draft")
	}
	var definition string
	if err := db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil ||
		!strings.Contains(definition, "v29_negative_replacement_draft_jcs") ||
		!strings.Contains(definition, "v29_perspective_replacement_detection_jcs") {
		t.Fatal("replacement GC reference is not installed", err)
	}
}

func TestV28DraftParamsReferenceRequiresSavedOwnerAndDigest(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"r.config_snapshot->'v28_local_portrait'->'intent'->>'task_id'=d.owner_id::text",
		"r.config_snapshot->'v28_local_portrait'->'intent'->>'params_sha'=d.sha256",
		"r.v28_donor_params_intent->>'owner_id'=d.owner_id::text",
		"r.v28_relight_intent->>'owner_id'=d.owner_id::text",
		"r.v28_restoration_params_intent->>'owner_id'=d.owner_id::text",
		"r.v28_donor_params_intent->>'params_sha'=d.sha256",
		"r.v28_relight_intent->>'params_sha'=d.sha256",
		"r.v28_restoration_params_intent->>'params_sha'=d.sha256",
		"r.kind='v2'", "r.status='draft'", "NOT r.cancel_requested",
	} {
		if !strings.Contains(definition, required) {
			t.Fatal("V28 draft Params GC reference missing", required)
		}
	}
}

func TestV29DraftParamsReferenceUsesMainPlanAndPreviewScope(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated migrated PostgreSQL required")
	}
	ctx := context.Background()
	db, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var definition string
	if err = db.QueryRow(ctx, `SELECT pg_get_functiondef('photo.v2_artifact_has_references(uuid,uuid)'::regprocedure)`).Scan(&definition); err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{
		"r.config_snapshot->'v29_multi_plan'->>'task_id'=d.owner_id::text",
		"r.config_snapshot->'v29_multi_plan'->>'params_sha'=d.sha256",
		"r.config_snapshot->'v29_perspective_plan'->>'task_id'=d.owner_id::text",
		"r.config_snapshot->'v29_perspective_plan'->>'params_sha'=d.sha256",
		"r.config_snapshot->'v29_perspective_lines_plan'->>'owner_id'=d.owner_id::text",
		"r.config_snapshot->'v29_perspective_lines_plan'->>'sha256'=d.sha256",
		"r.v29_perspective_line_detection IS TRUE",
		"r.config_snapshot->'v29_perspective_line_detection'->>'task_id'=d.owner_id::text",
		"d.v29_perspective_preview_params IS NOT NULL",
		"r.v29_perspective_preview_task_id IS NOT NULL",
		"d.v29_multi_preview_params IS NOT NULL",
		"r.v29_multi_preview_task_id IS NOT NULL",
		"d.v29_perspective_quality_params IS NOT NULL",
		"r.v29_perspective_quality_task_id IS NOT NULL",
		"d.v29_multi_quality_params IS NOT NULL",
		"r.v29_multi_quality_task_id IS NOT NULL",
		"r.kind='preview'", "r.status='draft'", "NOT r.cancel_requested",
	} {
		if !strings.Contains(definition, required) {
			t.Fatal("V29 draft Params GC reference missing", required)
		}
	}
}
