package semanticreview

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
	"time"
)

// Invoked only with a private physical worker/adopter fixture, not deployed data.
func TestIntegrationV27ReviewReadsAdoptedPlan(t *testing.T) {
	dsn := os.Getenv("PHOTO_TEST_DATABASE_URL")
	run := os.Getenv("PHOTO_TEST_V27_ASSEMBLY_RUN")
	if dsn == "" || run == "" {
		t.Skip("isolated adopted-plan fixture required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	owner, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal("fixture database unavailable")
	}
	defer owner.Close()
	var tenant, workflow, actor, session uuid.UUID
	var model, dataset string
	err = owner.QueryRow(ctx, `SELECT r.tenant_id,w.id,r.created_by,(r.config_snapshot->'v27_semantic'->>'auth_session_id')::uuid,
 r.config_snapshot->'v27_semantic'->'qualification'->>'model_lock_sha',r.config_snapshot->'v27_semantic'->'qualification'->>'dataset_sha'
 FROM photo.runs r JOIN photo.v2_workflows w ON w.tenant_id=r.tenant_id AND w.run_id=r.id WHERE r.id=$1`, run).Scan(&tenant, &workflow, &actor, &session, &model, &dataset)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("fixture database configuration invalid")
	}
	cfg.ConnConfig.RuntimeParams["role"] = "photo_api"
	api, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal("API database unavailable")
	}
	defer api.Close()
	admissionConfig, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("admission configuration unavailable")
	}
	admissionConfig.ConnConfig.RuntimeParams["role"] = "photo_semantic_admission"
	admission, err := pgxpool.NewWithConfig(ctx, admissionConfig)
	if err != nil {
		t.Fatal("admission database unavailable")
	}
	defer admission.Close()
	if err = VerifyExposureAdmissionRole(ctx, admission); err != nil {
		t.Fatal("restricted admission role unavailable", err)
	}
	if err = VerifyExposureAdmissionRole(ctx, owner); !errors.Is(err, ErrUnavailable) {
		t.Fatal("owner pool accepted as admission", err)
	}
	if err = VerifyExposureAdmissionRole(ctx, api); !errors.Is(err, ErrUnavailable) {
		t.Fatal("API pool accepted as admission", err)
	}
	var restrictedRole bool
	if err = owner.QueryRow(ctx, `SELECT
 NOT EXISTS(SELECT 1 FROM pg_roles WHERE rolname='photo_semantic_admission' AND (rolsuper OR rolcanlogin OR rolinherit OR rolbypassrls OR rolcreaterole OR rolcreatedb OR rolreplication))
 AND NOT EXISTS(SELECT 1 FROM pg_auth_members WHERE member='photo_semantic_admission'::regrole)
 AND NOT EXISTS(SELECT 1 FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='photo' AND c.relkind IN ('r','p') AND has_table_privilege('photo_semantic_admission',c.oid,'DELETE'))
 AND NOT has_table_privilege('photo_semantic_admission','photo.runs','INSERT')
 AND NOT has_table_privilege('photo_semantic_admission','photo.tasks','UPDATE')
 AND NOT has_table_privilege('photo_semantic_admission','photo.tenants','SELECT')
 AND NOT has_column_privilege('photo_semantic_admission','photo.idempotency_records','request_hash','UPDATE')
 AND has_column_privilege('photo_semantic_admission','photo.idempotency_records','response_body','UPDATE')
 AND has_function_privilege('photo_semantic_admission','photo.finish_semantic_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb,jsonb,jsonb,jsonb)','EXECUTE')
 AND NOT has_function_privilege('photo_api','photo.finish_semantic_apply_review(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,uuid,jsonb,jsonb,jsonb,jsonb)','EXECUTE')`).Scan(&restrictedRole); err != nil || !restrictedRole {
		t.Fatal("semantic admission role exceeds allowlist", err)
	}
	assertAdmissionReceiptIsolation(t, ctx, admission, tenant, actor)
	assertAdmissionRejectsDisabledRLS(t, ctx, owner)
	if err = VerifyExposureAdmissionRole(ctx, admission); err != nil {
		t.Fatal("RLS rollback did not restore admission", err)
	}
	bucket := os.Getenv("PHOTO_TEST_MINIO_BUCKET")
	store, err := objectstore.New(objectstore.Config{Endpoint: os.Getenv("PHOTO_TEST_MINIO_URL"), Bucket: bucket, AccessKey: os.Getenv("PHOTO_TEST_MINIO_ACCESS"), SecretKey: os.Getenv("PHOTO_TEST_MINIO_SECRET"), Development: true})
	if err != nil {
		t.Fatal(err)
	}
	read, err := NewPlanObjectReader(store, bucket)
	if err != nil {
		t.Fatal(err)
	}
	load := func(ctx context.Context) (PlanSources, error) {
		tx, err := api.Begin(ctx)
		if err != nil {
			return PlanSources{}, err
		}
		defer tx.Rollback(ctx)
		sources, err := LoadPlanSources(ctx, tx, tenant, workflow, actor, session)
		if err != nil {
			return PlanSources{}, err
		}
		if err = tx.Commit(ctx); err != nil {
			return PlanSources{}, err
		}
		return sources, nil
	}
	prepared, err := PrepareCurrentReview(ctx, load, read, 64<<20, model, dataset)
	expectedState := os.Getenv("PHOTO_TEST_V27_REVIEW_STATE")
	if expectedState == "" {
		expectedState = "proposed"
	}
	if expectedState != "proposed" && expectedState != "unsupported" {
		t.Fatal("invalid review fixture state")
	}
	if err != nil || len(prepared.Targets) != 1 || prepared.Targets[0].State != expectedState || prepared.Sources.RunID.String() != run {
		t.Fatal("review preparation failed", err)
	}
	proof, err := EncodeInitializationDocuments(prepared)
	if err != nil || len(proof) == 0 {
		t.Fatal("review document proof encoding failed", err)
	}
	tx, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	locked, err := LoadLockedPreparedSources(ctx, tx, tenant, workflow, actor, session, prepared)
	if err != nil || !EqualPlanSources(prepared.Sources, locked) {
		t.Fatal("locked review source fence failed", err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	var forged []initializationDocument
	if json.Unmarshal(proof, &forged) != nil {
		t.Fatal("proof decode")
	}
	for n := range forged {
		if forged[n].Contract == "SemanticPlan" {
			forged[n].Body = strings.Replace(forged[n].Body, `"schema_version":1`, `"schema_version":2`, 1)
		}
	}
	badProof, err := json.Marshal(forged)
	if err != nil {
		t.Fatal(err)
	}
	savepoint, err := tx.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var denied uuid.UUID
	err = savepoint.QueryRow(ctx, "SELECT photo.initialize_semantic_review($1,$2,$3,$4,$5,$6::jsonb)", tenant, workflow, actor, session, prepared.Sources.Revision, json.RawMessage(badProof)).Scan(&denied)
	var dbError *pgconn.PgError
	if !errors.As(err, &dbError) || dbError.Code != "23514" {
		t.Fatal("forged proof accepted", err)
	}
	if err = savepoint.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	var before int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_intents WHERE tenant_id=$1 AND workflow_id=$2", tenant, workflow).Scan(&before); err != nil || before != 0 {
		t.Fatal("partial initializer write", err, before)
	}
	id, err := InitializePreparedReview(ctx, tx, tenant, workflow, actor, session, prepared)
	if err != nil {
		t.Fatal("review initialization failed", err)
	}
	replayed, err := InitializePreparedReview(ctx, tx, tenant, workflow, actor, session, prepared)
	if err != nil || replayed != id {
		t.Fatal("review initialization replay failed", err)
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = tx.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_targets WHERE tenant_id=$1 AND intent_id=$2 AND state=$3 AND revision=1 AND child_run_id IS NULL AND batch_id IS NULL", tenant, id, expectedState).Scan(&count); err != nil || count != 1 {
		t.Fatal("review initial target mismatch", err, count)
	}
	lockedTargets, err := LoadLockedTargets(ctx, tx, tenant, workflow, actor, session, prepared.Sources.Revision)
	if err != nil || len(lockedTargets) != 1 {
		t.Fatal("locked decision inventory failed", err)
	}
	target := lockedTargets[0]
	if target.State != expectedState || target.Revision != 1 || target.ChildRunID != nil || target.BatchID != nil || target.AssetVersionID != prepared.Targets[0].AssetVersionID {
		t.Fatal("locked decision target mismatch")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	assertInitializedRunIntent(t, ctx, owner, api, auth.Identity{UserID: actor, SessionID: session}, tenant, prepared.Sources.RunID, workflow, id, fmt.Sprint(prepared.Sources.Revision))
	second, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Rollback(ctx)
	committedReplay, err := InitializePreparedReview(ctx, second, tenant, workflow, actor, session, prepared)
	if err != nil || committedReplay != id {
		t.Fatal("committed initializer replay failed", err)
	}
	if err = second.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if expectedState == "proposed" {
		preparationRequest := ApplyDecision{PlanSHA: prepared.PlanRef.SHA256, ExpectedRevision: fmt.Sprint(prepared.Sources.Revision), TargetDecisions: []TargetDecision{{TargetID: lockedTargets[0].ID, Decision: "approve"}}}
		serviceReview, serviceTargets, e := PrepareScopedApplyReview(ctx, api, auth.Identity{UserID: actor, SessionID: session}, tenant, id, preparationRequest, read, 64<<20, model, dataset)
		if e != nil || !EqualPlanSources(serviceReview.Sources, prepared.Sources) || len(serviceTargets) != 1 || serviceTargets[0].ID != lockedTargets[0].ID {
			t.Fatal("scoped service preparation", e)
		}
	}
	decisionTX, err := api.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer decisionTX.Rollback(ctx)
	absentReplay, err := ReadApplyReplay(ctx, decisionTX, tenant, id, actor, session, "physical-unused-key", strings.Repeat("a", 64))
	if err != nil || absentReplay.Outcome != "absent" || len(absentReplay.Body) != 0 {
		t.Fatal("physical Go replay read failed", err)
	}
	currentTargets, err := LoadLockedTargets(ctx, decisionTX, tenant, workflow, actor, session, prepared.Sources.Revision)
	if err != nil || len(currentTargets) != 1 {
		t.Fatal("independent decision lock failed", err)
	}
	if expectedState == "proposed" {
		request := ApplyDecision{PlanSHA: prepared.PlanRef.SHA256, ExpectedRevision: fmt.Sprint(prepared.Sources.Revision), TargetDecisions: []TargetDecision{{TargetID: currentTargets[0].ID, Decision: "approve"}}}
		requestReplay, err := ReadRequestReplay(ctx, decisionTX, tenant, id, actor, session, "physical-unused-request-key", request)
		if err != nil || requestReplay.Outcome != "absent" {
			t.Fatal("authenticated request replay failed", err)
		}
		edits, err := CompileApprovedEdits(prepared, currentTargets, request)
		if err != nil || len(edits) != 1 || edits[0].TargetID != currentTargets[0].ID || edits[0].Source.AssetVersionID != currentTargets[0].AssetVersionID || edits[0].Candidate.ArtifactID != *currentTargets[0].CandidateArtifactID || edits[0].Mask.ArtifactID == uuid.Nil || edits[0].Transform.ArtifactID == uuid.Nil || edits[0].Kind != prepared.Intent.Kind || edits[0].Strength != prepared.Intent.Strength || !edits[0].PreservePersons {
			t.Fatal("physical approved edit compilation failed", err)
		}
		decisionProof, err := BuildDecisionProof(prepared, currentTargets, request)
		if err != nil || len(decisionProof.Approved) != 1 || decisionProof.Approved[0] != currentTargets[0].ID {
			t.Fatal("physical decision proof failed", err)
		}
		var doc decisionDocument
		if json.Unmarshal(decisionProof.Body, &doc) != nil || len(doc.Targets) != 1 || doc.Targets[0].Candidate.ArtifactID != *currentTargets[0].CandidateArtifactID || doc.Targets[0].Source.AssetVersionID != currentTargets[0].AssetVersionID {
			t.Fatal("physical decision binding mismatch")
		}
	}
	probe, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = probe.Exec(ctx, "SET LOCAL lock_timeout='150ms'"); err != nil {
		t.Fatal(err)
	}
	var probeID uuid.UUID
	err = probe.QueryRow(ctx, "SELECT id FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, currentTargets[0].ID).Scan(&probeID)
	var lockError *pgconn.PgError
	if !errors.As(err, &lockError) || lockError.Code != "55P03" {
		t.Fatal("decision target lock not held", err)
	}
	if err = probe.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if err = decisionTX.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = owner.QueryRow(ctx, "SELECT id FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$2 FOR UPDATE", tenant, currentTargets[0].ID).Scan(&probeID); err != nil || probeID != currentTargets[0].ID {
		t.Fatal("decision target lock not released", err)
	}
	if expectedState == "proposed" {
		request := ApplyDecision{PlanSHA: prepared.PlanRef.SHA256, ExpectedRevision: fmt.Sprint(prepared.Sources.Revision), TargetDecisions: []TargetDecision{{TargetID: currentTargets[0].ID, Decision: "approve"}}}
		var storageBefore, reservedBefore int64
		if err = owner.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&storageBefore, &reservedBefore); err != nil {
			t.Fatal(err)
		}
		published, err := PublishDecision(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request)
		if err != nil {
			t.Fatal("physical decision publication failed", err)
		}
		checkDecisionGate := func(req ApplyDecision, publication PublishedDecision) {
			t.Helper()
			proof, proofErr := BuildDecisionProof(prepared, currentTargets, req)
			if proofErr != nil {
				t.Fatal(proofErr)
			}
			fingerprint, _ := canonical.Marshal(struct {
				RunID   uuid.UUID     `json:"run_id"`
				Request ApplyDecision `json:"request"`
			}{prepared.Sources.RunID, req})
			documents, docErr := EncodeInitializationDocuments(prepared)
			if docErr != nil {
				t.Fatal(docErr)
			}
			gateTX, gateErr := owner.Begin(ctx)
			if gateErr != nil {
				t.Fatal(gateErr)
			}
			defer gateTX.Rollback(ctx)
			args := []any{tenant, id, actor, session, prepared.Sources.Revision, string(fingerprint), publication.Ref.ArtifactID, string(proof.Body), json.RawMessage(documents)}
			query := "SELECT photo.lock_semantic_decision_batch($1,$2,$3,$4,$5,$6,$7,$8,$9)"
			var batchID uuid.UUID
			if gateErr = gateTX.QueryRow(ctx, query, args...).Scan(&batchID); gateErr != nil || batchID != publication.BatchID {
				t.Fatal("published decision gate", gateErr)
			}
			for _, index := range []int{4, 6, 7} {
				changed := append([]any(nil), args...)
				switch index {
				case 4:
					changed[index] = prepared.Sources.Revision + 1
				case 6:
					changed[index] = uuid.New()
				case 7:
					changed[index] = "{}"
				}
				probeTX, probeErr := gateTX.Begin(ctx)
				if probeErr != nil {
					t.Fatal(probeErr)
				}
				probeErr = probeTX.QueryRow(ctx, query, changed...).Scan(&batchID)
				probeTX.Rollback(ctx)
				if probeErr == nil {
					t.Fatal("changed decision gate accepted", index)
				}
			}
		}
		checkDecisionGate(request, published)
		republished, err := PublishDecision(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request)
		if err != nil || republished.BatchID != published.BatchID || republished.Ref != published.Ref {
			t.Fatal("decision publication replay failed", err)
		}
		var source PlanSource
		source.Ref = published.Ref
		if err = owner.QueryRow(ctx, "SELECT bytes,bucket,object_key,object_version,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND verified_at IS NOT NULL AND output_role='review_selection'", tenant, published.Ref.ArtifactID).Scan(&source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType); err != nil {
			t.Fatal(err)
		}
		body, err := read(ctx, source)
		if err != nil || !bytes.Equal(body, published.Proof.Body) {
			t.Fatal("published decision bytes mismatch", err)
		}
		var storageAfter, reservedAfter int64
		if err = owner.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&storageAfter, &reservedAfter); err != nil || storageAfter-storageBefore != int64(len(body)) || reservedAfter != reservedBefore {
			t.Fatal("decision quota accounting mismatch", err)
		}

		child := uuid.New()
		drafts, err := PublishApprovedTaskParams(ctx, api, store, bucket, tenant, id, actor, session, child, prepared, currentTargets, request)
		if err != nil || len(drafts) != 1 {
			t.Fatal("physical Params publication failed", err)
		}
		again, err := PublishApprovedTaskParams(ctx, api, store, bucket, tenant, id, actor, session, child, prepared, currentTargets, request)
		if err != nil || len(again) != 1 || again[0].Params != drafts[0].Params {
			t.Fatal("Params replay failed", err)
		}
		paramsSource := PlanSource{Ref: drafts[0].Params}
		if err = owner.QueryRow(ctx, "SELECT bytes,bucket,object_key,object_version,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND output_role='task_params' AND verified_at IS NOT NULL", tenant, drafts[0].Params.ArtifactID).Scan(&paramsSource.Bytes, &paramsSource.Bucket, &paramsSource.Key, &paramsSource.Version, &paramsSource.MediaType); err != nil {
			t.Fatal(err)
		}
		paramsBody, err := read(ctx, paramsSource)
		if err != nil || !bytes.Equal(paramsBody, drafts[0].Body) {
			t.Fatal("physical Params bytes differ", err)
		}
		var paramsStorage, paramsReserved int64
		if err = owner.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&paramsStorage, &paramsReserved); err != nil || paramsStorage-storageAfter != int64(len(paramsBody)) || paramsReserved != reservedAfter {
			t.Fatal("Params quota replay mismatch", err)
		}
		// Physical transport publication only: this one-entry probe does not
		// claim a complete executable edit closure or qualified child admission.
		transport, err := NewApprovedTransportPublisher(api, store, bucket, tenant, id, actor, session, prepared.Sources.RunID, child, prepared.Sources.Revision, currentTargets[0], drafts[0])
		if err != nil {
			t.Fatal("pre-child transport publisher", err)
		}
		entries := []v2input.Entry{{Ref: drafts[0].Params, Contract: "Params"}}
		chunkBody, _ := canonical.Marshal(v2input.Chunk{SchemaVersion: 1, Entries: entries})
		chunk, err := transport.Publish(ctx, drafts[0].TaskID, 0, "InputClosureChunk", chunkBody)
		if err != nil {
			t.Fatal("physical transport chunk", err)
		}
		entriesSHA, _ := canonical.Hash(entries)
		manifestBody, _ := canonical.Marshal(v2input.Manifest{SchemaVersion: 1, TaskID: drafts[0].TaskID, EntryCount: 1, EntriesSHA: entriesSHA, Chunks: []v2input.Ref{chunk}})
		manifest, err := transport.Publish(ctx, drafts[0].TaskID, 1, "InputClosureManifest", manifestBody)
		if err != nil {
			t.Fatal("physical transport manifest", err)
		}
		for _, item := range []struct {
			ref            v2input.Ref
			body           []byte
			ordinal        int
			contract, role string
		}{
			{chunk, chunkBody, 0, "InputClosureChunk", "v2_input_chunk_0"}, {manifest, manifestBody, 1, "InputClosureManifest", "v2_input_manifest"},
		} {
			again, err := transport.Publish(ctx, drafts[0].TaskID, item.ordinal, item.contract, item.body)
			if err != nil || again != item.ref {
				t.Fatal("physical transport replay", err)
			}
			source := PlanSource{Ref: item.ref}
			if err = owner.QueryRow(ctx, "SELECT bytes,bucket,object_key,object_version,media_type FROM photo.artifacts WHERE tenant_id=$1 AND id=$2 AND output_role=$3 AND verified_at IS NOT NULL", tenant, item.ref.ArtifactID, item.role).Scan(&source.Bytes, &source.Bucket, &source.Key, &source.Version, &source.MediaType); err != nil {
				t.Fatal(err)
			}
			body, err := read(ctx, source)
			if err != nil || !bytes.Equal(body, item.body) {
				t.Fatal("physical transport bytes", err)
			}
		}
		var transportStorage, transportReserved int64
		if err = owner.QueryRow(ctx, "SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&transportStorage, &transportReserved); err != nil || transportStorage-paramsStorage != int64(len(chunkBody)+len(manifestBody)) || transportReserved != paramsReserved {
			t.Fatal("transport quota replay", err)
		}
		transportProjection := []ExposureTransportBinding{{TaskID: drafts[0].TaskID, EntryCount: 1, EntriesSHA: entriesSHA,
			Manifest: manifest, Chunks: []v2input.Ref{chunk}, DirectRoots: []v2input.Ref{drafts[0].Params}}}
		transportNodes := []map[string]any{{"id": drafts[0].TaskID, "params_artifact_id": drafts[0].Params.ArtifactID, "params_sha": drafts[0].Params.SHA256}}
		checkTransport := func(nodes []map[string]any, projection []ExposureTransportBinding) bool {
			t.Helper()
			nodesRaw, err := canonical.Marshal(nodes)
			if err != nil {
				t.Fatal(err)
			}
			transportRaw, err := canonical.Marshal(projection)
			if err != nil {
				t.Fatal(err)
			}
			var ready bool
			if err = owner.QueryRow(ctx, "SELECT photo.semantic_approved_transport_batch_ready($1,$2,$3::jsonb,$4::jsonb)", tenant, prepared.Sources.RunID, json.RawMessage(nodesRaw), json.RawMessage(transportRaw)).Scan(&ready); err != nil {
				t.Fatal("physical transport SQL gate", err)
			}
			return ready
		}
		if !checkTransport(transportNodes, transportProjection) {
			t.Fatal("Go transport projection rejected by SQL")
		}
		// Pure digest agreement only; this probe does not qualify exposure.
		inputDigest, err := canonical.Hash(struct {
			Entries   []v2input.Entry `json:"entries"`
			Operation string          `json:"operation"`
			Task      uuid.UUID       `json:"task"`
			Version   string          `json:"version"`
		}{entries, "semantic.local_exposure", drafts[0].TaskID, "1"})
		if err != nil {
			t.Fatal(err)
		}
		entriesRaw, _ := canonical.Marshal(entries)
		var digestReady bool
		if err = owner.QueryRow(ctx, "SELECT photo.semantic_exposure_inventory_digest_ready($1,$2,$3,$4,$5,$6::jsonb)", drafts[0].TaskID, drafts[0].Params.ArtifactID, drafts[0].Params.SHA256, inputDigest, entriesSHA, json.RawMessage(entriesRaw)).Scan(&digestReady); err != nil || !digestReady {
			t.Fatal("Go/SQL inventory digest disagreement", err)
		}
		wrongNodes := []map[string]any{{"id": drafts[0].TaskID, "params_artifact_id": drafts[0].Params.ArtifactID, "params_sha": strings.Repeat("0", 64)}}
		if checkTransport(wrongNodes, transportProjection) {
			t.Fatal("changed Params SHA accepted by SQL")
		}
		wrongProjection := append([]ExposureTransportBinding(nil), transportProjection...)
		wrongProjection[0].DirectRoots = []v2input.Ref{prepared.IntentRef}
		if checkTransport(transportNodes, wrongProjection) {
			t.Fatal("ready unrelated direct root replaced Params")
		}
		var childCount int
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, child).Scan(&childCount); err != nil || childCount != 0 {
			t.Fatal("transport gate created child", err)
		}

		commitInputs, err := PrepareApprovedCommitInputs(tenant, actor, child, prepared, currentTargets, request, published, drafts)
		if err != nil || len(commitInputs.Nodes) != 1 || commitInputs.Nodes[0].TaskID != drafts[0].TaskID || !bytes.Equal(commitInputs.Documents, proof) {
			t.Fatal("physical approved commit input mismatch", err)
		}
		paramProof := []map[string]any{{"task_id": drafts[0].TaskID, "target_id": drafts[0].TargetID, "artifact_id": drafts[0].Params.ArtifactID, "sha256": drafts[0].Params.SHA256, "body": string(drafts[0].Body)}}
		checkParams := func(rows []map[string]any, decisions []TargetDecision) bool {
			t.Helper()
			encoded, err := json.Marshal(rows)
			if err != nil {
				t.Fatal(err)
			}
			selected, err := json.Marshal(decisions)
			if err != nil {
				t.Fatal(err)
			}
			var allowed bool
			if err = owner.QueryRow(ctx, "SELECT photo.semantic_approved_params_ready($1,$2,$3,$4::jsonb,$5::jsonb,$6::jsonb)", tenant, prepared.Sources.RunID, id, json.RawMessage(selected), json.RawMessage(encoded), json.RawMessage(proof)).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			return allowed
		}

		encodedParams, _ := json.Marshal(paramProof)
		encodedDecisions, _ := json.Marshal(request.TargetDecisions)
		for _, candidateChild := range []uuid.UUID{child, uuid.New(), uuid.Nil, prepared.Sources.RunID} {
			var allowed bool
			if err = owner.QueryRow(ctx, "SELECT photo.semantic_approved_child_params_ready($1,$2,$3,$4,$5::jsonb,$6::jsonb,$7::jsonb)", tenant, prepared.Sources.RunID, id, candidateChild, json.RawMessage(encodedDecisions), json.RawMessage(encodedParams), json.RawMessage(proof)).Scan(&allowed); err != nil {
				t.Fatal(err)
			}
			if allowed != (candidateChild == child) {
				t.Fatal("child identity binding mismatch")
			}
		}
		if !checkParams(paramProof, request.TargetDecisions) {
			t.Fatal("published Params commit pins denied")
		}
		for _, field := range []string{"task_id", "target_id", "artifact_id", "sha256", "body"} {
			changed := make(map[string]any)
			for key, value := range paramProof[0] {
				changed[key] = value
			}
			if field == "sha256" {
				changed[field] = strings.Repeat("0", 64)
			} else if field == "body" {
				changed[field] = "{}"
			} else {
				changed[field] = uuid.New()
			}
			if checkParams([]map[string]any{changed}, request.TargetDecisions) {
				t.Fatal("substituted Params accepted", field)
			}
		}
		if checkParams(append(paramProof, paramProof[0]), request.TargetDecisions) {
			t.Fatal("duplicate Params accepted")
		}

		if pinRaw := os.Getenv("PHOTO_TEST_V27_EXPOSURE_PIN"); pinRaw != "" {
			var preparedProject uuid.UUID
			if err = owner.QueryRow(ctx, "SELECT project_id FROM photo.semantic_intents WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&preparedProject); err != nil {
				t.Fatal(err)
			}
			var pin ExposureQualificationPin
			if json.Unmarshal([]byte(pinRaw), &pin) != nil || !validExposureQualification(pin) {
				t.Fatal("fixture CPU pin invalid")
			}
			cost, costErr := CompileExposureResources(256, 256, ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}, ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000})
			if costErr != nil {
				t.Fatal(costErr)
			}
			profileRaw, _ := canonical.Marshal(ExposureExecutionProfile{Backend: "cpu", RendererVersion: "v2.2", ResourceEstimate: cost, Qualification: pin, ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA})
			nodesRaw, _ := canonical.Marshal([]v2input.DraftTask{{ID: drafts[0].TaskID, NodeKey: "semantic.edit:" + drafts[0].TargetID.String(), Operation: "semantic.local_exposure", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: drafts[0].Params.ArtifactID, ParamsSHA: drafts[0].Params.SHA256, InputDigest: inputDigest, ExecutionProfile: profileRaw}})
			fingerprint, _ := canonical.Marshal(struct {
				RunID   uuid.UUID     `json:"run_id"`
				Request ApplyDecision `json:"request"`
			}{prepared.Sources.RunID, request})
			shellQuery := "SELECT photo.create_semantic_apply_draft_shell($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)"
			shellArgs := []any{tenant, id, actor, session, prepared.Sources.Revision, string(fingerprint), published.Ref.ArtifactID, string(published.Proof.Body), json.RawMessage(commitInputs.Documents), child, json.RawMessage(commitInputs.Params), json.RawMessage(nodesRaw)}
			for _, index := range []int{9, 11} {
				changed := append([]any(nil), shellArgs...)
				if index == 9 {
					changed[index] = uuid.New()
				} else {
					var altered []v2input.DraftTask
					json.Unmarshal(nodesRaw, &altered)
					var profile map[string]any
					json.Unmarshal(altered[0].ExecutionProfile, &profile)
					profile["backend"] = "cuda"
					altered[0].ExecutionProfile, _ = canonical.Marshal(profile)
					badNodes, _ := canonical.Marshal(altered)
					changed[index] = json.RawMessage(badNodes)
				}
				deniedTX, deniedErr := owner.Begin(ctx)
				if deniedErr != nil {
					t.Fatal(deniedErr)
				}
				var deniedBatch uuid.UUID
				deniedErr = deniedTX.QueryRow(ctx, shellQuery, changed...).Scan(&deniedBatch)
				deniedTX.Rollback(ctx)
				if deniedErr == nil {
					t.Fatal("invalid draft shell accepted", index)
				}
			}
			quotaDeniedTX, quotaDeniedErr := owner.Begin(ctx)
			if quotaDeniedErr != nil {
				t.Fatal(quotaDeniedErr)
			}
			if _, quotaDeniedErr = quotaDeniedTX.Exec(ctx, "UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}',to_jsonb((SELECT storage_bytes+reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1))) WHERE id=$1", tenant); quotaDeniedErr != nil {
				t.Fatal(quotaDeniedErr)
			}
			var quotaDeniedBatch uuid.UUID
			quotaDeniedErr = quotaDeniedTX.QueryRow(ctx, shellQuery, shellArgs...).Scan(&quotaDeniedBatch)
			var quotaError *pgconn.PgError
			if !errors.As(quotaDeniedErr, &quotaError) || quotaError.Message != "SEMANTIC_APPLY_QUOTA_EXCEEDED" {
				t.Fatal("quota exhausted shell accepted or wrong denial", quotaDeniedErr)
			}
			quotaDeniedTX.Rollback(ctx)
			if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, child).Scan(&childCount); err != nil || childCount != 0 {
				t.Fatal("quota denial leaked draft", err)
			}
			shellTX, shellErr := owner.Begin(ctx)
			if shellErr != nil {
				t.Fatal(shellErr)
			}
			var batch uuid.UUID
			shellErr = shellTX.QueryRow(ctx, shellQuery, shellArgs...).Scan(&batch)
			if shellErr != nil || batch != published.BatchID {
				shellTX.Rollback(ctx)
				t.Fatal("approved draft shell", shellErr)
			}
			var state, configSHA string
			var sealed bool
			var config json.RawMessage
			if shellErr = shellTX.QueryRow(ctx, "SELECT status,graph_sealed,config_snapshot,config_hash FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, child).Scan(&state, &sealed, &config, &configSHA); shellErr != nil || state != "draft" || sealed {
				t.Fatal("draft run state", shellErr)
			}
			var configValue any
			json.Unmarshal(config, &configValue)
			expectedSHA, _ := canonical.Hash(configValue)
			if expectedSHA != configSHA {
				t.Fatal("shell config JCS digest mismatch")
			}
			var taskState, targetState string
			var targetChild uuid.UUID
			if shellErr = shellTX.QueryRow(ctx, "SELECT state FROM photo.tasks WHERE tenant_id=$1 AND id=$2", tenant, drafts[0].TaskID).Scan(&taskState); shellErr != nil || taskState != "blocked" {
				t.Fatal("draft task activated", shellErr)
			}
			if shellErr = shellTX.QueryRow(ctx, "SELECT state,child_run_id FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$2", tenant, drafts[0].TargetID).Scan(&targetState, &targetChild); shellErr != nil || targetState != "queued" || targetChild != child {
				t.Fatal("draft target binding", shellErr)
			}
			var childQuota int
			var parentRevision int64
			if shellErr = shellTX.QueryRow(ctx, "SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_id=$2", tenant, child).Scan(&childQuota); shellErr != nil || childQuota != 1 {
				t.Fatal("shell quota missing or duplicated", shellErr)
			}
			var reservedBeforeReplay, reservedAfterReplay, amount int64
			if shellErr = shellTX.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&reservedBeforeReplay); shellErr != nil {
				t.Fatal(shellErr)
			}
			if shellErr = shellTX.QueryRow(ctx, "SELECT photo.reserve_semantic_apply_quota($1,$2,$3,$4,$5,$6)", tenant, preparedProject, actor, child, json.RawMessage(commitInputs.Params), json.RawMessage(nodesRaw)).Scan(&amount); shellErr != nil || amount != cost.OutputBytes {
				t.Fatal("quota replay budget", shellErr)
			}
			if shellErr = shellTX.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", tenant).Scan(&reservedAfterReplay); shellErr != nil || reservedBeforeReplay != reservedAfterReplay {
				t.Fatal("quota replay double accounting", shellErr)
			}
			for _, mutation := range []string{
				"UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_id=$2",
				"UPDATE photo.quota_reservations SET bytes=bytes-1 WHERE tenant_id=$1 AND holder_id=$2",
			} {
				deniedTX, deniedErr := shellTX.Begin(ctx)
				if deniedErr != nil {
					t.Fatal(deniedErr)
				}
				if _, deniedErr = deniedTX.Exec(ctx, mutation, tenant, child); deniedErr != nil {
					t.Fatal(deniedErr)
				}
				deniedErr = deniedTX.QueryRow(ctx, "SELECT photo.reserve_semantic_apply_quota($1,$2,$3,$4,$5,$6)", tenant, preparedProject, actor, child, json.RawMessage(commitInputs.Params), json.RawMessage(nodesRaw)).Scan(&amount)
				deniedTX.Rollback(ctx)
				if deniedErr == nil {
					t.Fatal("invalid quota replay accepted")
				}
			}
			if shellErr = shellTX.QueryRow(ctx, "SELECT revision FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, prepared.Sources.RunID).Scan(&parentRevision); shellErr != nil || parentRevision != prepared.Sources.Revision {
				t.Fatal("shell advanced parent before final commit", shellErr)
			}
			if shellErr = shellTX.Rollback(ctx); shellErr != nil {
				t.Fatal(shellErr)
			}
			if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, child).Scan(&childCount); err != nil || childCount != 0 {
				t.Fatal("shell rollback leaked child", err)
			}
		}

		if protectionRaw := os.Getenv("PHOTO_TEST_V27_EXPOSURE_PROTECTION"); protectionRaw != "" {
			var protection v2input.Ref
			var pin ExposureQualificationPin
			if json.Unmarshal([]byte(protectionRaw), &protection) != nil || json.Unmarshal([]byte(os.Getenv("PHOTO_TEST_V27_EXPOSURE_PIN")), &pin) != nil {
				t.Fatal("physical exposure fixture pin")
			}
			discovered, discoveryErr := ReadApprovedProtectionPins(ctx, api, tenant, id, actor, session, prepared, currentTargets, request)
			if discoveryErr != nil || discovered[currentTargets[0].ID] != protection {
				t.Fatal("authorized protection pin discovery", discoveryErr)
			}
			fullChild := uuid.New()
			fullDrafts, err := PublishApprovedTaskParams(ctx, api, store, bucket, tenant, id, actor, session, fullChild, prepared, currentTargets, request)
			if err != nil || len(fullDrafts) != 1 {
				t.Fatal("full exposure Params", err)
			}
			var edit ApprovedEdit
			json.Unmarshal(fullDrafts[0].Body, &edit)
			protectionDocument, err := ReadApprovedProtectionDocument(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, protection, read)
			if err != nil {
				t.Fatal("scoped protection object read", err)
			}
			var protectionRoot struct {
				Artifact v2input.Ref `json:"artifact"`
			}
			if json.Unmarshal(protectionDocument.Body, &protectionRoot) != nil {
				t.Fatal("protection binary pin")
			}
			originQuery := "SELECT photo.semantic_approved_protection_origin($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)"
			originArgs := []any{tenant, id, actor, session, prepared.Sources.Revision, currentTargets[0].ID, currentTargets[0].Revision, edit.Source.AssetVersionID, edit.Candidate.ArtifactID, edit.Candidate.SHA256, protection.ArtifactID, protection.SHA256, protectionRoot.Artifact.ArtifactID, protectionRoot.Artifact.SHA256, string(protectionDocument.Body)}
			verifiedBinary, originErr := VerifyApprovedProtectionOrigin(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, protectionDocument)
			if originErr != nil || verifiedBinary != protectionRoot.Artifact {
				t.Fatal("Go protected producer guard", originErr)
			}
			verifiedBinarySource, binaryErr := VerifyApprovedProtectionBinary(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, protectionDocument)
			if binaryErr != nil || verifiedBinarySource.Ref != verifiedBinary {
				t.Fatal("scoped physical protection NPY", binaryErr)
			}
			var originReady bool
			if err = api.QueryRow(ctx, originQuery, originArgs...).Scan(&originReady); err != nil || !originReady {
				t.Fatal("approved protection canonical origin", err)
			}
			var binarySource PlanSource
			binaryQuery := "SELECT contract,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.semantic_approved_protection_binary($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)"
			if err = api.QueryRow(ctx, binaryQuery, originArgs...).Scan(&binarySource.Contract, &binarySource.Ref.ArtifactID, &binarySource.Ref.SHA256, &binarySource.Bytes, &binarySource.Bucket, &binarySource.Key, &binarySource.Version, &binarySource.MediaType); err != nil || binarySource.Ref != verifiedBinary || binarySource.Contract != "NpyArray" || binarySource.Bytes < 32 || binarySource.Version == "" {
				t.Fatal("scoped protection binary metadata", err)
			}
			changedBinaryArgs := append([]any(nil), originArgs...)
			changedBinaryArgs[12] = uuid.New()
			if err = api.QueryRow(ctx, binaryQuery, changedBinaryArgs...).Scan(&binarySource.Contract, &binarySource.Ref.ArtifactID, &binarySource.Ref.SHA256, &binarySource.Bytes, &binarySource.Bucket, &binarySource.Key, &binarySource.Version, &binarySource.MediaType); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("foreign binary metadata disclosed", err)
			}
			for _, index := range []int{12, 13, 14} {
				changed := append([]any(nil), originArgs...)
				switch index {
				case 12:
					changed[index] = uuid.New()
				case 13:
					changed[index] = strings.Repeat("0", 64)
				case 14:
					changed[index] = string(protectionDocument.Body) + " "
				}
				if err = api.QueryRow(ctx, originQuery, changed...).Scan(&originReady); err != nil || originReady {
					t.Fatal("changed protection proof accepted", index, err)
				}
			}
			originDeniedTX, originDeniedErr := owner.Begin(ctx)
			if originDeniedErr != nil {
				t.Fatal(originDeniedErr)
			}
			if _, originDeniedErr = originDeniedTX.Exec(ctx, "UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE tenant_id=$1 AND id=(SELECT created_by_task_id FROM photo.artifacts WHERE tenant_id=$1 AND id=$2)", tenant, protection.ArtifactID); originDeniedErr != nil {
				t.Fatal(originDeniedErr)
			}
			if _, originDeniedErr = originDeniedTX.Exec(ctx, "SET LOCAL ROLE photo_api"); originDeniedErr != nil {
				t.Fatal(originDeniedErr)
			}
			originDeniedErr = originDeniedTX.QueryRow(ctx, originQuery, originArgs...).Scan(&originReady)
			originDeniedTX.Rollback(ctx)
			if originDeniedErr != nil || originReady {
				t.Fatal("stale protected producer fence accepted", originDeniedErr)
			}
			sourceDocument, err := ReadApprovedImageDocument(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, false, read)
			if err != nil {
				t.Fatal("scoped source image", err)
			}
			referenceDocument, err := ReadApprovedImageDocument(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, true, read)
			if err != nil {
				t.Fatal("scoped reference image", err)
			}
			for _, reference := range []bool{false, true} {
				imageDocument := sourceDocument
				if reference {
					imageDocument = referenceDocument
				}
				pixels, err := VerifyApprovedImageArray(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "pixels", -1)
				if err != nil || pixels.Contract != "NpyArray" {
					t.Fatal("scoped image pixels metadata", reference, err)
				}
				brokenImageStore := &planStoreFixture{Body: []byte("corrupt")}
				if found, err := VerifyApprovedImageArray(ctx, api, brokenImageStore, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "pixels", -1); err == nil || found != (PlanSource{}) || brokenImageStore.Closed != 1 {
					t.Fatal("corrupt RGB accepted or stream leaked", err)
				}
				parsedImage, err := review.ParseWorkingImage(imageDocument.Body)
				if err != nil {
					t.Fatal(err)
				}
				if parsedImage.Alpha != nil {
					alpha, err := VerifyApprovedImageArray(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "alpha", -1)
					if err != nil || alpha.Ref.ArtifactID != parsedImage.Alpha.ID {
						t.Fatal("scoped legacy alpha", err)
					}
				} else if _, err := ResolveApprovedImageBinaryMetadata(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "alpha", -1); err == nil {
					t.Fatal("absent alpha exposed")
				}
				var sourceChain struct {
					Steps []struct {
						Kind  string       `json:"kind"`
						Field *v2input.Ref `json:"inverse_field"`
					}
				}
				if json.Unmarshal(parsedImage.TransformChain, &sourceChain) != nil {
					t.Fatal("source chain")
				}
				if len(sourceChain.Steps) == 0 {
					if _, err := ResolveApprovedImageBinaryMetadata(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "inverse_field", 0); err == nil {
						t.Fatal("absent geometry exposed")
					}
				} else {
					for step, value := range sourceChain.Steps {
						if value.Kind == "dense_warp" {
							field, err := VerifyApprovedImageArray(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "inverse_field", step)
							if err != nil || value.Field == nil || field.Ref != *value.Field {
								t.Fatal("scoped dense geometry", err)
							}
							mediaProbeStore := &separationProbeStore{store: store}
							if result, err := VerifyApprovedImageArray(ctx, imageBinaryMediaProbeQuery{db: api}, mediaProbeStore, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "inverse_field", step); err == nil || result != (PlanSource{}) || mediaProbeStore.opened != 0 {
								t.Fatal("wrong dense media opened or accepted", err)
							}
							corruptFieldStore := &separationProbeStore{store: store, corruptAt: 1}
							if result, err := VerifyApprovedImageArray(ctx, api, corruptFieldStore, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "inverse_field", step); err == nil || result != (PlanSource{}) || corruptFieldStore.closed != 1 {
								t.Fatal("corrupt dense field accepted or leaked", err)
							}

						}
					}
					if _, err := ResolveApprovedImageBinaryMetadata(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, reference, imageDocument, "inverse_field", len(sourceChain.Steps)); err == nil {
						t.Fatal("unbound geometry step exposed")
					}
				}
				var exposed uuid.UUID
				if err := api.QueryRow(ctx, "SELECT artifact_id FROM photo.semantic_approved_image_binary($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)", tenant, id, actor, session, prepared.Sources.Revision, edit.TargetID, currentTargets[0].Revision, edit.Source.AssetVersionID, edit.Candidate.ArtifactID, edit.Candidate.SHA256, reference, imageDocument.Source.Ref.ArtifactID, imageDocument.Source.Ref.SHA256, string(imageDocument.Body)+" ", "pixels", -1).Scan(&exposed); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("changed image bytes exposed child", err)
				}
			}
			maskDocument, err := ReadApprovedCandidateDocument(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, false, read)
			if err != nil {
				t.Fatal("scoped transferred mask", err)
			}
			maskPixels, err := VerifyApprovedMaskBinary(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, maskDocument)
			if err != nil || maskPixels.Contract != "NpyArray" {
				t.Fatal("scoped transferred mask pixels", err)
			}
			for _, scenario := range []struct {
				name                                                          string
				failAt, nilAt, corruptAt, closeFailAt, queryFailAt, versionAt int
				cancel                                                        bool
				closes                                                        int
			}{
				{"first-open", 1, 0, 0, 0, 0, 0, false, 0},
				{"second-open", 2, 0, 0, 0, 0, 0, false, 1},
				{"second-nil", 0, 2, 0, 0, 0, 0, false, 1},
				{"corrupt-selected", 0, 0, 1, 0, 0, 0, false, 2},
				{"corrupt-protected", 0, 0, 2, 0, 0, 0, false, 2},
				{"close-selected", 0, 0, 0, 1, 0, 0, false, 2},
				{"close-protected", 0, 0, 0, 2, 0, 0, false, 2},
				{"reauthorization", 0, 0, 0, 0, 4, 0, false, 2},
				{"changed-version", 0, 0, 0, 0, 0, 4, false, 2},
				{"cancel-after-read", 0, 0, 0, 0, 0, 0, true, 2},
			} {
				probeCtx, probeCancel := context.WithCancel(ctx)
				probeDB := &separationProbeQuery{db: api, failAt: scenario.queryFailAt, versionAt: scenario.versionAt}
				probeStore := &separationProbeStore{store: store, failAt: scenario.failAt, nilAt: scenario.nilAt, corruptAt: scenario.corruptAt, closeFailAt: scenario.closeFailAt}
				if scenario.cancel {
					probeStore.cancel = probeCancel
				}
				coverage, probeErr := VerifyApprovedExposureMaskSeparation(probeCtx, probeDB, probeStore, bucket, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, maskDocument, protectionDocument)
				probeCancel()
				if probeErr == nil || coverage != 0 || probeStore.closed != scenario.closes {
					t.Fatal("separation failure leaked proof or stream", scenario.name, coverage, probeStore.closed, probeErr)
				}
				if scenario.queryFailAt != 0 && !errors.Is(probeErr, separationProbeError) {
					t.Fatal("reauthorization refusal lost", probeErr)
				}
				if scenario.versionAt != 0 && !errors.Is(probeErr, ErrConflict) {
					t.Fatal("changed post-IO version accepted", probeErr)
				}
				if scenario.cancel && !errors.Is(probeErr, context.Canceled) {
					t.Fatal("post-read cancellation lost", probeErr)
				}
			}
			transformDocument, err := ReadApprovedCandidateDocument(ctx, api, tenant, id, actor, session, prepared, currentTargets, request, edit.TargetID, true, read)
			if err != nil || transformDocument.Source.Ref != edit.Transform {
				t.Fatal("scoped transferred transform", err)
			}
			if err := ValidateApprovedExposureGeometry(prepared, currentTargets, request, edit.TargetID, sourceDocument.Body, referenceDocument.Body, maskDocument.Body, transformDocument.Body); err != nil {
				t.Fatal("original exposure geometry", err)
			}
			changedTransform := append(append([]byte(nil), transformDocument.Body...), ' ')
			if err := ValidateApprovedExposureGeometry(prepared, currentTargets, request, edit.TargetID, sourceDocument.Body, referenceDocument.Body, maskDocument.Body, changedTransform); err == nil {
				t.Fatal("changed transform bytes accepted")
			}
			var candidateBody []byte
			for _, document := range prepared.documents {
				if document.Source.Ref == edit.Candidate {
					candidateBody = document.Body
				}
			}
			maskBinaryArgs := []any{tenant, id, actor, session, prepared.Sources.Revision, edit.TargetID, currentTargets[0].Revision, edit.Source.AssetVersionID, edit.Candidate.ArtifactID, edit.Candidate.SHA256, string(candidateBody), string(maskDocument.Body)}
			maskBinaryQuery := "SELECT artifact_id FROM photo.semantic_approved_mask_binary($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)"
			for _, index := range []int{10, 11} {
				changed := append([]any(nil), maskBinaryArgs...)
				changed[index] = changed[index].(string) + " "
				var exposed uuid.UUID
				if err = api.QueryRow(ctx, maskBinaryQuery, changed...).Scan(&exposed); !errors.Is(err, pgx.ErrNoRows) {
					t.Fatal("altered mask binary proof exposed object", index, err)
				}
			}
			staleMaskTX, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = staleMaskTX.Exec(ctx, "UPDATE photo.artifacts SET produced_fence=produced_fence+1 WHERE tenant_id=$1 AND id=$2", tenant, maskPixels.Ref.ArtifactID); err != nil {
				staleMaskTX.Rollback(ctx)
				t.Fatal(err)
			}
			if _, err = staleMaskTX.Exec(ctx, "SET LOCAL ROLE photo_api"); err != nil {
				staleMaskTX.Rollback(ctx)
				t.Fatal(err)
			}
			var exposedMask uuid.UUID
			err = staleMaskTX.QueryRow(ctx, maskBinaryQuery, maskBinaryArgs...).Scan(&exposedMask)
			staleMaskTX.Rollback(ctx)
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("stale mask pixel fence exposed object", err)
			}
			var deniedChild uuid.UUID
			if err = api.QueryRow(ctx, "SELECT artifact_id FROM photo.semantic_approved_candidate_child($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)", tenant, id, actor, session, prepared.Sources.Revision, edit.TargetID, currentTargets[0].Revision, edit.Source.AssetVersionID, edit.Candidate.ArtifactID, edit.Candidate.SHA256, string(candidateBody)+" ", "mask").Scan(&deniedChild); !errors.Is(err, pgx.ErrNoRows) {
				t.Fatal("changed candidate bytes exposed child", err)
			}
			documents := []ExposureTargetDocuments{{TargetID: edit.TargetID, Protection: protection,
				Documents:  ExposureSourceDocuments{Source: sourceDocument.Body, Reference: referenceDocument.Body, Mask: maskDocument.Body, Protection: protectionDocument.Body},
				OwnerCosts: ExposureOwnerCosts{RuntimeRAMBytes: 1 << 20, InputIndexScratchBytes: 16 << 20}, Limits: ExposureResourceEstimate{RAMBytes: 1 << 40, ScratchBytes: 1 << 40, OutputBytes: 1 << 40, PixelCount: 200000000}, Qualification: pin}}
			resolved, err := ResolveApprovedExposureDocumentInputs(fullChild, prepared, currentTargets, request, documents)
			if err != nil {
				t.Fatal("physical exposure document inputs", err)
			}
			iccRaw, err := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
			if err != nil {
				t.Fatal(err)
			}
			colorProfile, err := NewExposureColorProfile(iccRaw)
			if err != nil {
				t.Fatal(err)
			}
			pinRaw, _ := json.Marshal(pin)
			var reportEvidence json.RawMessage
			evidenceQuery := "SELECT photo.semantic_exposure_review_report_evidence($1,$2,$3,$4,$5,$6)"
			if err = api.QueryRow(ctx, evidenceQuery, tenant, id, actor, session, prepared.Sources.Revision, pinRaw).Scan(&reportEvidence); err != nil || len(reportEvidence) == 0 {
				t.Fatal("pre-child qualification evidence unavailable", err)
			}
			var evidence struct {
				Pin    ExposureQualificationPin `json:"pin"`
				Report struct {
					SHA string `json:"sha256"`
				} `json:"report"`
			}
			if err = json.Unmarshal(reportEvidence, &evidence); err != nil || evidence.Pin != pin || evidence.Report.SHA != pin.Report.SHA256 {
				t.Fatal("qualification evidence substituted pin", err)
			}
			for _, invalid := range []struct {
				tenant, session uuid.UUID
				revision        int64
			}{{uuid.New(), session, prepared.Sources.Revision}, {tenant, uuid.New(), prepared.Sources.Revision}, {tenant, session, prepared.Sources.Revision + 1}} {
				var denied json.RawMessage
				err = api.QueryRow(ctx, evidenceQuery, invalid.tenant, id, actor, invalid.session, invalid.revision, pinRaw).Scan(&denied)
				if err == nil && len(denied) != 0 {
					t.Fatal("foreign/stale report evidence exposed")
				}
			}
			sourceScope, err := PrepareApprovedExposureSourceScope(ctx, api, store, bucket, tenant, id, actor, session, fullChild, prepared, currentTargets, request, []ExposureInputBudget{{TargetID: edit.TargetID, OwnerCosts: documents[0].OwnerCosts, Limits: documents[0].Limits, Qualification: pin}}, colorProfile)
			if err != nil {
				t.Fatal("authenticated complete exposure preflight", err)
			}
			batchInputs, err := sourceScope.Inputs()
			if err != nil {
				t.Fatal(err)
			}
			originalInputs, _ := json.Marshal(resolved)
			batchBytes, _ := json.Marshal(batchInputs)
			if string(originalInputs) != string(batchBytes) {
				t.Fatal("batch resolver changed closure inputs")
			}
			resolved = batchInputs
			frozen, err := CompileApprovedExposureTasks(fullChild, prepared, currentTargets, request, resolved)
			if err != nil || len(frozen) != 1 {
				t.Fatal("physical exposure closure", err)
			}
			publisher, err := NewApprovedExposurePublisher(api, store, bucket, tenant, id, actor, session, prepared.Sources.RunID, fullChild, prepared.Sources.Revision, currentTargets[0], frozen[0])
			if err != nil {
				t.Fatal(err)
			}
			fullPublished, err := PublishExposureTask(ctx, frozen[0], publisher)
			if err != nil || fullPublished.Graph.Built.Count < 8 {
				t.Fatal("full exposure transport", err)
			}
			serviceBundle, err := PrepareExposureApply(ctx, api, store, bucket, auth.Identity{UserID: actor, SessionID: session}, tenant, id, fullChild, request, read, 64<<20, model, dataset,
				[]ExposureInputBudget{{TargetID: edit.TargetID, OwnerCosts: documents[0].OwnerCosts, Limits: documents[0].Limits, Qualification: pin}}, colorProfile)
			if err != nil || serviceBundle.Child != fullChild || len(serviceBundle.Tasks) != 1 || serviceBundle.Decision.BatchID != published.BatchID || !EqualPlanSources(serviceBundle.Review.Sources, prepared.Sources) {
				t.Fatal("complete exposure service preparation", err)
			}
			serviceTask, _ := json.Marshal(serviceBundle.Tasks[0])
			originalTask, _ := json.Marshal(fullPublished)
			if !bytes.Equal(serviceTask, originalTask) {
				t.Fatal("service preparation changed published exposure task")
			}
			fullTX, err := owner.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer fullTX.Rollback(context.Background())
			finishKey := "exposure-finish-" + fullChild.String()
			finishRequest, _ := canonical.Marshal(struct {
				RunID   uuid.UUID     `json:"run_id"`
				Request ApplyDecision `json:"request"`
			}{prepared.Sources.RunID, request})
			finishHash, _ := canonical.Hash(json.RawMessage(finishRequest))
			if _, err = fullTX.Exec(ctx, "INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) VALUES($1,'user',$2,'SemanticIntent.Apply',$3,$4,clock_timestamp()+interval '30 days')", tenant, actor, finishKey, finishHash); err != nil {
				t.Fatal("finish receipt", err)
			}

			var project uuid.UUID
			if err = fullTX.QueryRow(ctx, "SELECT project_id FROM photo.semantic_intents WHERE tenant_id=$1 AND id=$2", tenant, id).Scan(&project); err != nil {
				t.Fatal(err)
			}
			if _, err = fullTX.Exec(ctx, "SELECT id FROM photo.tenants WHERE id=$1 FOR UPDATE", tenant); err != nil {
				t.Fatal(err)
			}
			if _, err = fullTX.Exec(ctx, "SELECT id FROM photo.tenant_usage WHERE tenant_id=$1 FOR UPDATE", tenant); err != nil {
				t.Fatal(err)
			}
			fullInputs, err := CreateSourceVerifiedExposureDraftInTransaction(ctx, fullTX, tenant, project, id, actor, session, fullChild, prepared, currentTargets, request, published, sourceScope, []PreparedExposureTask{fullPublished})
			if err != nil {
				fullTX.Rollback(ctx)
				t.Fatal("full exposure draft and seal", err)
			}
			var ready bool
			if err = fullTX.QueryRow(ctx, "SELECT photo.semantic_apply_sealed_inventory_ready($1,$2,$3,$4,$5,$6)", tenant, project, fullChild, fullInputs.Nodes, fullInputs.Transport, fullInputs.Inventory).Scan(&ready); err != nil || !ready {
				t.Fatal("full persisted exposure seal", err)
			}
			var inputRows int
			if err = fullTX.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", tenant, fullDrafts[0].TaskID).Scan(&inputRows); err != nil || inputRows != fullPublished.Graph.Built.Count+len(fullPublished.Graph.Built.Chunks)+1 {
				t.Fatal("full exposure inventory count", err)
			}
			var fullQuota int64
			if err = fullTX.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2 AND state='reserved'", tenant, fullChild).Scan(&fullQuota); err != nil || fullQuota != int64(12*resolved[0].Width*resolved[0].Height+16384+3*1048576) {
				t.Fatal("full exposure reserved budget", err)
			}
			activationSQL := "SELECT photo.activate_semantic_apply_child($1,$2,$3,$4,$5,$6,$7,$8,$9)"
			activationArgs := []any{tenant, project, prepared.Sources.RunID, fullChild, actor, fullInputs.Review.Params, fullInputs.Nodes, fullInputs.Transport, fullInputs.Inventory}
			mutations := []struct {
				sql  string
				args []any
			}{
				{"UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=$2", []any{tenant, prepared.Sources.RunID}},
				{"UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=$1 AND id=$2", []any{tenant, fullChild}},
				{"UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_id=$2", []any{tenant, fullChild}},
				{"UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=(SELECT (execution_profile->'qualification'->>'policy_id')::uuid FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 LIMIT 1)", []any{tenant, fullChild}},
			}
			for _, mutation := range mutations {
				probe, e := fullTX.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = probe.Exec(ctx, mutation.sql, mutation.args...); e != nil {
					probe.Rollback(ctx)
					t.Fatal("activation denial setup", e)
				}
				var admitted bool
				if e = probe.QueryRow(ctx, activationSQL, activationArgs...).Scan(&admitted); e != nil || admitted {
					probe.Rollback(ctx)
					t.Fatal("activation denial", e)
				}
				var unchanged bool
				if e = probe.QueryRow(ctx, "SELECT status='draft' AND NOT graph_sealed AND NOT EXISTS(SELECT 1 FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state<>'blocked') FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, fullChild).Scan(&unchanged); e != nil || !unchanged {
					probe.Rollback(ctx)
					t.Fatal("activation denial changed child", e)
				}
				if e = probe.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
			}
			var finishBody []byte
			if err = fullTX.QueryRow(ctx, "SELECT photo.finish_semantic_apply_review($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)", tenant, id, actor, session, finishKey, prepared.Sources.Revision, string(finishRequest), published.Ref.ArtifactID, string(published.Proof.Body), fullChild, fullInputs.Review.Params, fullInputs.Nodes, fullInputs.Transport, fullInputs.Inventory).Scan(&finishBody); err != nil {
				t.Fatal("full exposure review finish", err)
			}
			finishResult, e := DecodeBatchResult(finishBody)
			if e != nil || finishResult.Child == nil || finishResult.Child.RunID != fullChild || finishResult.BatchID != published.BatchID {
				t.Fatal("full exposure finish result", e)
			}
			var atomic bool
			if err = fullTX.QueryRow(ctx, "SELECT r.revision=$3+1 AND r.status='waiting_review' AND i.response_status=200 AND i.response_body=$5::jsonb AND EXISTS(SELECT 1 FROM photo.run_events WHERE tenant_id=$1 AND run_id=r.id AND type='v27.review.batch' AND payload=$5::jsonb) FROM photo.runs r JOIN photo.idempotency_records i ON i.tenant_id=r.tenant_id AND i.key=$4 WHERE r.tenant_id=$1 AND r.id=$2", tenant, prepared.Sources.RunID, prepared.Sources.Revision, finishKey, finishBody).Scan(&atomic); err != nil || !atomic {
				t.Fatal("full exposure finish atomic receipt/revision/event", err)
			}

			var activated bool
			if err = fullTX.QueryRow(ctx, "SELECT graph_sealed AND status='queued' FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, fullChild).Scan(&activated); err != nil || !activated {
				t.Fatal("full exposure activation state", err)
			}
			var readyTasks int
			if err = fullTX.QueryRow(ctx, "SELECT count(*) FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='ready' AND attempt=0", tenant, fullChild).Scan(&readyTasks); err != nil || readyTasks != 1 {
				t.Fatal("full exposure activation tasks", err)
			}
			fullTX.Rollback(ctx)
			commitTX, e := owner.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer commitTX.Rollback(context.Background())
			committed, e := CommitApprovedExposure(ctx, commitTX, tenant, id, actor, session, serviceBundle.Child, finishKey, serviceBundle.Review, serviceBundle.Targets, request, serviceBundle.Decision, serviceBundle.SourceScope, serviceBundle.Tasks)
			if e != nil || committed.Replayed || committed.Status != 200 {
				t.Fatal("approved exposure coordinator", e)
			}
			replayed, e := CommitApprovedExposure(ctx, commitTX, tenant, id, actor, session, uuid.New(), finishKey, PreparedReview{}, nil, request, PublishedDecision{}, PreparedExposureSourceScope{}, nil)
			if e != nil || !replayed.Replayed || !bytes.Equal(committed.Body, replayed.Body) {
				t.Fatal("approved exposure receipt replay", e)
			}
			changed := request
			changed.TargetDecisions = append([]TargetDecision(nil), request.TargetDecisions...)
			changed.TargetDecisions[0].Decision = "reject"
			conflictTX, e := commitTX.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			_, e = CommitApprovedExposure(ctx, conflictTX, tenant, id, actor, session, fullChild, finishKey, prepared, currentTargets, changed, published, sourceScope, []PreparedExposureTask{fullPublished})
			var conflict *domain.Error
			if !errors.As(e, &conflict) || conflict.Code != "IDEMPOTENCY_CONFLICT" || conflict.HTTPStatus != 409 {
				conflictTX.Rollback(ctx)
				t.Fatal("approved changed request conflict", e)
			}
			if e = conflictTX.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			resolvedTX, e := commitTX.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			_, e = CommitApprovedExposure(ctx, resolvedTX, tenant, id, actor, session, fullChild, finishKey+"-new", prepared, currentTargets, request, published, sourceScope, []PreparedExposureTask{fullPublished})
			if !errors.Is(e, ErrConflict) {
				resolvedTX.Rollback(ctx)
				t.Fatal("approved resolved target conflict", e)
			}
			if e = resolvedTX.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			if os.Getenv("PHOTO_TEST_V27_COMMITTED_REPLAY") == "true" {
				if e = commitTX.Rollback(ctx); e != nil {
					t.Fatal(e)
				}
				selectionCalls := 0
				unavailable := ApplyService{API: api, Prepare: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) (PreparedApply, error) {
					selectionCalls++
					return PreparedApply{}, ErrInvalid
				}}
				missingResponse, missingErr := unavailable.Apply(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, id, finishKey, request)
				if !errors.Is(missingErr, ErrUnavailable) || selectionCalls != 0 || missingResponse.Status != 0 {
					t.Fatal("missing admission must fail before preparation", missingErr)
				}
				catalog := ExposurePreparationCatalog{SchemaVersion: 1, Tenants: []ExposureTenantPreparation{{TenantID: tenant,
					Review: ReviewPreparationConfig{SchemaVersion: 1, ModelSHA: model, DatasetSHA: dataset, DocumentBudgetBytes: 64 << 20},
					Execution: ExposureDeploymentBudget{Qualification: pin, OwnerCosts: documents[0].OwnerCosts, Limits: documents[0].Limits,
						ModelLockSHA: pin.ModelLockSHA, RuntimeLockSHA: pin.RuntimeLockSHA, DeviceProfileSHA: pin.DeviceProfileSHA, DatasetSHA: pin.DatasetSHA}}}}
				for _, duplicateTenant := range []uuid.UUID{tenant, uuid.New()} {
					invalid := catalog
					other := catalog.Tenants[0]
					other.TenantID = duplicateTenant
					invalid.Tenants = []ExposureTenantPreparation{catalog.Tenants[0], other}
					invalidRaw, _ := json.Marshal(invalid)
					if _, e = DecodeExposurePreparationCatalog(invalidRaw); !errors.Is(e, ErrInvalid) {
						t.Fatal("duplicate tenant or cross-tenant report accepted", e)
					}
				}
				freshService, e := InstallTenantExposureApplyService(ctx, ExposureApplyInstallation{API: api, Admission: admission, Objects: store, Bucket: bucket, Color: colorProfile}, catalog)
				if e != nil {
					t.Fatal("exposure service installation", e)
				}
				catalog.Tenants[0].TenantID = uuid.New()
				catalog.Tenants[0].Execution.Qualification.Backend = "cuda"
				if handoff := os.Getenv("PHOTO_TEST_V27_PREPARED_APPLY_FILE"); handoff != "" {
					raw, e := json.Marshal(struct {
						Tenant, Intent, Actor, Session uuid.UUID
						Key                            string
						Request                        ApplyDecision
					}{tenant, id, actor, session, finishKey, request})
					if e != nil || os.WriteFile(handoff, raw, 0600) != nil {
						t.Fatal("prepared fixture handoff failed")
					}
					t.Log("V27_REVIEW_PREPARED_OK")
					t.Log("V27_REVIEW_READ_OK")
					return
				}
				committed, e = freshService.Apply(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, id, finishKey, request)
				if e != nil || committed.Replayed || committed.Status != 200 || request.TargetDecisions[0].Decision != "approve" {
					t.Fatal("fresh approved service commit", e)
				}
				freshResult, e := DecodeBatchResult(committed.Body)
				if e != nil || freshResult.Child == nil || freshResult.Child.RunID == fullChild || freshResult.BatchID != published.BatchID {
					t.Fatal("fresh approved service child identity", e)
				}
				fullChild = freshResult.Child.RunID
				var freshTask uuid.UUID
				if e = owner.QueryRow(ctx, "SELECT id FROM photo.tasks WHERE tenant_id=$1 AND run_id=$2 AND state='ready' AND attempt=0", tenant, fullChild).Scan(&freshTask); e != nil {
					t.Fatal("fresh service ready task", e)
				}
				replayTX, e := owner.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer replayTX.Rollback(context.Background())
				durable, e := CommitApprovedExposure(ctx, replayTX, tenant, id, actor, session, uuid.New(), finishKey, PreparedReview{}, nil, request, PublishedDecision{}, PreparedExposureSourceScope{}, nil)
				if e != nil || !durable.Replayed || !bytes.Equal(durable.Body, committed.Body) {
					t.Fatal("durable approved receipt replay", e)
				}
				if e = replayTX.Commit(ctx); e != nil {
					t.Fatal(e)
				}
				apiReplayTX, e := api.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer apiReplayTX.Rollback(context.Background())
				apiReceipt, e := ReadRequestReplay(ctx, apiReplayTX, tenant, id, actor, session, finishKey, request)
				if e != nil || apiReceipt.Outcome != "replayed" || !bytes.Equal(apiReceipt.Body, committed.Body) {
					t.Fatal("API durable receipt replay", e)
				}
				apiReplayTX.Rollback(ctx)
				prepareCalled := false
				service := ApplyService{API: api, Prepare: func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, ApplyDecision) (PreparedApply, error) {
					prepareCalled = true
					return PreparedApply{}, errors.New("replay must not prepare sources")
				}}
				serviceReceipt, e := service.Apply(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, id, finishKey, request)
				if e != nil || prepareCalled || !serviceReceipt.Replayed || serviceReceipt.Status != 200 || serviceReceipt.ResourceID == nil || *serviceReceipt.ResourceID != published.BatchID || !bytes.Equal(serviceReceipt.Body, committed.Body) {
					t.Fatal("service replay must precede source preparation and admission", e)
				}
				var durableCount int
				if e = owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_review_batches WHERE tenant_id=$1 AND child_run_id=$2", tenant, fullChild).Scan(&durableCount); e != nil || durableCount != 1 {
					t.Fatal("durable replay duplicated child batch", e)
				}
				if e = owner.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v27.review.batch' AND payload->>'batch_id'=$3", tenant, prepared.Sources.RunID, published.BatchID.String()).Scan(&durableCount); e != nil || durableCount != 1 {
					t.Fatal("durable replay duplicated event", e)
				}
				assertExposureRegisteredInputEvidence(t, ctx, owner, tenant, fullChild, freshTask)
				t.Log("V27_REVIEW_READ_OK")
				return
			}
			if e = commitTX.Rollback(ctx); e != nil {
				t.Fatal(e)
			}
			if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, fullChild).Scan(&childCount); err != nil || childCount != 0 {
				t.Fatal("full draft rollback leaked", err)
			}
			var quotaRows int
			if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", tenant, fullChild).Scan(&quotaRows); err != nil || quotaRows != 0 {
				t.Fatal("full draft rollback leaked quota", err)
			}

		}

		request.TargetDecisions[0].Decision = "reject"
		rejection, err := PublishDecision(ctx, api, store, bucket, tenant, id, actor, session, prepared, currentTargets, request)
		if err != nil {
			t.Fatal("rejection publication failed", err)
		}
		checkDecisionGate(request, rejection)
		var runsBefore, quotaBefore int
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1", tenant).Scan(&runsBefore); err != nil {
			t.Fatal(err)
		}
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1", tenant).Scan(&quotaBefore); err != nil {
			t.Fatal(err)
		}
		rejectPreparation, err := NewRejectionPreparation(api, store, read, bucket, 64<<20, model, dataset)
		if err != nil {
			t.Fatal(err)
		}
		rejectService := ApplyService{API: api, Prepare: rejectPreparation}
		response, err := rejectService.Apply(ctx, auth.Identity{UserID: actor, SessionID: session}, tenant, id, "physical-reject-key", request)
		if err != nil || response.Status != 200 || response.Replayed {
			t.Fatal("atomic rejection commit failed", err)
		}
		result, err := DecodeBatchResult(response.Body)
		if err != nil || result.Remaining != 0 || result.Child != nil || result.BatchID != rejection.BatchID || result.Revision != fmt.Sprint(prepared.Sources.Revision+1) {
			t.Fatal("rejection result mismatch", err)
		}
		replayTX, err := api.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer replayTX.Rollback(ctx)
		replayed, err := ReadRequestReplay(ctx, replayTX, tenant, id, actor, session, "physical-reject-key", request)
		if err != nil || replayed.Outcome != "replayed" || !bytes.Equal(replayed.Body, response.Body) {
			t.Fatal("committed rejection replay failed", err)
		}
		methodReplay, err := CommitRejections(ctx, replayTX, tenant, id, actor, session, "physical-reject-key", prepared, currentTargets, request, rejection)
		if err != nil || !methodReplay.Replayed || !bytes.Equal(methodReplay.Body, response.Body) {
			t.Fatal("commit method replay changed result", err)
		}
		if err = replayTX.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		staleTX, err := api.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer staleTX.Rollback(ctx)
		_, err = CommitRejections(ctx, staleTX, tenant, id, actor, session, "physical-new-key-resolved", prepared, currentTargets, request, rejection)
		var staleError *pgconn.PgError
		if !errors.As(err, &staleError) || staleError.Code != "40001" {
			t.Fatal("new key repeated resolved decision", err)
		}
		if err = staleTX.Rollback(ctx); err != nil {
			t.Fatal(err)
		}
		var partialReceipts, eventCount int
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.idempotency_records WHERE tenant_id=$1 AND route='SemanticIntent.Apply' AND key='physical-new-key-resolved'", tenant).Scan(&partialReceipts); err != nil || partialReceipts != 0 {
			t.Fatal("failed rejection retained partial receipt", err)
		}
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v27.review.batch'", tenant, prepared.Sources.RunID).Scan(&eventCount); err != nil || eventCount != 1 {
			t.Fatal("replay duplicated batch event", err)
		}
		var state, parentState string
		var revision int64
		var runsAfter, quotaAfter int
		if err = owner.QueryRow(ctx, "SELECT state,revision FROM photo.semantic_targets WHERE tenant_id=$1 AND id=$2", tenant, currentTargets[0].ID).Scan(&state, &revision); err != nil || state != "rejected" || revision != 2 {
			t.Fatal("rejection target mismatch", err)
		}
		if err = owner.QueryRow(ctx, "SELECT status,revision FROM photo.runs WHERE tenant_id=$1 AND id=$2", tenant, prepared.Sources.RunID).Scan(&parentState, &revision); err != nil || parentState != "waiting_review" || revision != prepared.Sources.Revision+1 {
			t.Fatal("parent prematurely completed", err)
		}
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1", tenant).Scan(&runsAfter); err != nil {
			t.Fatal(err)
		}
		if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.quota_reservations WHERE tenant_id=$1", tenant).Scan(&quotaAfter); err != nil || runsAfter != runsBefore || quotaAfter != quotaBefore {
			t.Fatal("reject batch admitted child/quota", err)
		}
	}
	if err = owner.QueryRow(ctx, "SELECT count(*) FROM photo.semantic_targets WHERE tenant_id=$1 AND intent_id=$2", tenant, id).Scan(&count); err != nil || count != 1 {
		t.Fatal("committed target mismatch", err, count)
	}
	t.Log("V27_REVIEW_READ_OK")
}
