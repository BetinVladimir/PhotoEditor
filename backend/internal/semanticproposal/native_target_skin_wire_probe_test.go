package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"testing"
	"time"
)

func probeNativeTargetSkinPlan(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, parser nativeTargetFaceDraftPlan, parserBuilt, detectorBuilt v2input.Built, owner *pgxpool.Pool) {
	t.Helper()

	if os.Getenv("PHOTO_TEST_V27_TARGET_SKIN_ADVANCE") == "1" {
		factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
		advance := func(want string) {
			t.Helper()
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				out, err := p.advanceNativeTargetSkins(work, who, original, parser.detector.anchor.manifest, factory)
				if err != nil {
					return err
				}
				if out.phase != nativePreparationWaiting || out.operation != want {
					return v2input.ErrConflict
				}
				return nil
			})
			if err != nil || !ok {
				t.Fatal("automatic target skin advance", want, ok, err)
			}
		}
		advance("mask.face_skin")
		advance("mask.face_skin")
		a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, parser.detector.anchor.manifest, parser.detector.anchor.entry.Ordinal, "mask.face_skin")
		if err != nil {
			t.Fatal(err)
		}
		saved, err := p.Reader.readNativeTargetSkinSaved(ctx, who, a)
		if err != nil || !saved.Sealed {
			t.Fatal("automatic target skin saved", err)
		}
		if probeNativeTargetFailureIfRequested(t, ctx, p, who, original, saved.Run, saved.Task, "mask.face_skin") {
			return
		}
		probeNativeWireTaskOutputs(t, ctx, saved.Task)
		advance("semantic.transfer")
		advance("semantic.transfer")
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_ADVANCE") == "1" {
			if os.Getenv("PHOTO_TEST_V27_NATIVE_ALL_TARGET_TRANSFER") == "1" {
				probeNativeTargetDefectsReadyForTransfer(t, ctx, p, who, original, parser.detector.anchor.manifest, parser.detector.anchor.entry.Ordinal)
			} else {
				probeNativeTargetDefectAdvance(t, ctx, p, who, original, parser.detector.anchor.manifest, parser.detector.anchor.entry.Ordinal, owner)
			}
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_PARAMS") == "1" {
			current, err := p.Reader.readNativeTargetSkinSaved(ctx, who, a)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := p.Reader.restoreNativeTargetSkinPlan(ctx, who, parser, a, p.Configuration.Color, current)
			if err != nil {
				t.Fatal(err)
			}
			probeNativeTargetDefectParams(t, ctx, p, who, original, plan, owner)
		}
		t.Log("V27_TARGET_SKIN_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK")
		return
	}
	cfg, err := p.Reader.selectInitialFacePipelineConfiguration(ctx, who, parser.shell.Tenant, p.Configuration, p.ReservedBytes, p.TransportDocuments)
	if err != nil {
		t.Fatal(err)
	}
	params, err := p.Reader.publishNativeTargetSkinParams(ctx, who, parser, p.Configuration.Color, p.Documents)
	if err != nil {
		t.Fatal("target skin params", err)
	}
	prepared, err := p.Reader.prepareNativeTargetSkinTask(ctx, who, parser, p.Configuration.Color, params, cfg.Skin.RAM, cfg.Skin.Scratch, cfg.Skin.Output)
	if err != nil {
		t.Fatal("target skin task", err)
	}
	plan, err := p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, prepared, cfg.Skin.Reserved)
	if err != nil {
		t.Fatal("target skin plan", err)
	}
	var queueConfig map[string]json.RawMessage
	if json.Unmarshal(plan.shell.Config, &queueConfig) != nil || string(queueConfig["queue_class"]) != `"batch"` { t.Fatal("target skin queue missing") }
	oldPrepared := prepared
	oldPrepared.legacyQueueOmitted = true
	oldPlan, err := p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, oldPrepared, cfg.Skin.Reserved)
	if err != nil { t.Fatal("historical target skin compile", err) }
	var oldConfig map[string]json.RawMessage
	if json.Unmarshal(oldPlan.shell.Config, &oldConfig) != nil { t.Fatal("historical target skin config") }
	if _, present := oldConfig["queue_class"]; present { t.Fatal("historical target skin queue fabricated") }
	oldReplay, err := p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, oldPlan.prepared, cfg.Skin.Reserved)
	if err != nil || oldReplay.seal != oldPlan.seal || string(oldReplay.shell.Config) != string(oldPlan.shell.Config) { t.Fatal("historical target skin replay changed", err) }
	again, err := p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, prepared, cfg.Skin.Reserved)
	if err != nil || again.seal != plan.seal {
		t.Fatal("target skin deterministic replay", err)
	}
	for _, kind := range []string{"namespace", "actor", "params", "parser", "pixels", "result", "budget", "body"} {
		changed := prepared
		reserve := cfg.Skin.Reserved
		switch kind {
		case "namespace":
			changed.parent = uuid.New()
		case "actor":
			changed.actor = uuid.New()
		case "params":
			changed.params.document = uuid.New()
		case "parser":
			changed.params.parser.planSeal = "changed"
		case "pixels":
			changed.params.parser.pixels.ref.ArtifactID = uuid.New()
		case "result":
			changed.params.parser.catalog.ResultID = uuid.New()
		case "budget":
			reserve = 1
		case "body":
			changed.params.parser.body = "{}"
		}
		if _, err := p.Reader.compileNativeTargetSkinDraftPlan(ctx, who, parser, changed, reserve); err == nil {
			t.Fatal("target skin substitution accepted", kind)
		}
	}
	transport, _, err := publishNativeTargetSkinDraftTransport(ctx, plan, &proposalMemoryTransport{})
	if err != nil || transport.Graph.ID != plan.shell.Tasks[0].ID {
		t.Fatal("target skin memory transport", err)
	}
	changed := plan
	changed.prepared.params.parser.pixels.object.Bytes++
	if err := verifyNativeTargetSkinPlanSeal(ctx, changed); err == nil {
		t.Fatal("target skin fingerprint mutation accepted")
	}
	t.Log("V27_TARGET_SKIN_PHYSICAL_FACTORY_COMPILER_OK")

	begin := func(want string) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				out, err := p.Reader.beginNativeTargetSkinDraftInTransaction(work, tx, who, original, plan, p.Configuration.Color, parserBuilt, detectorBuilt)
				if err != nil {
					return err
				}
				if out.RunID != plan.shell.Run || out.Outcome != want {
					return v2input.ErrConflict
				}
				return nil
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.runs SET status='running' WHERE tenant_id=$1 AND id=$2", parser.shell.Tenant, parser.shell.Run); err != nil {
		t.Fatal(err)
	}
	uncompleted := begin("created")
	if _, err := owner.Exec(ctx, "UPDATE photo.runs SET status='completed' WHERE tenant_id=$1 AND id=$2", parser.shell.Tenant, parser.shell.Run); err != nil {
		t.Fatal(err)
	}
	if uncompleted == nil {
		t.Fatal("target skin consumed unfinished parser run")
	}
	var leaked int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.runs WHERE tenant_id=$1 AND id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&leaked); err != nil || leaked != 0 {
		t.Fatal("target skin failed begin leaked draft", leaked, err)
	}
	for _, want := range []string{"created", "pending"} {
		if err := begin(want); err != nil {
			t.Fatal("target skin blocked begin/replay", want, err)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	refused := begin("pending")
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if refused == nil {
		t.Fatal("target skin revoked begin accepted")
	}
	if err := begin("pending"); err != nil {
		t.Fatal("target skin restored begin", err)
	}
	var status, state string
	var sealed bool
	var queue int
	if err := owner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state,(SELECT count(*) FROM photo.outbox o WHERE o.tenant_id=r.tenant_id AND o.payload->>'run_id'=r.id::text AND o.routing_key='v2.run.queued') FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&status, &sealed, &state, &queue); err != nil || status != "draft" || sealed || state != "blocked" || queue != 0 {
		t.Fatal("target skin blocked state", status, sealed, state, queue, err)
	}
	t.Log("V27_TARGET_SKIN_BLOCKED_BEGIN_REPLAY_REVOKE_OK")

	published, _, err := publishNativeTargetSkinDraftTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: plan.shell.Tenant, Run: plan.shell.Run})
	if err != nil {
		t.Fatal("target skin physical transport", err)
	}
	register := func() error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.registerNativeTargetSkinInputsInTransaction(work, tx, who, original, plan, p.Configuration.Color, parserBuilt, detectorBuilt, published.Graph.Built)
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := register(); err != nil {
			t.Fatal("target skin registered replay", i, err)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied := register()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if denied == nil {
		t.Fatal("revoked target skin registration accepted")
	}
	if err := register(); err != nil {
		t.Fatal("restored target skin registration", err)
	}
	t.Log("V27_TARGET_SKIN_REGISTRATION_REPLAY_REVOKE_OK")

	packet, err := p.Reader.marshalNativeTargetSkinAdmissionEvidence(ctx, who, original, plan, published.Graph.Built, parserBuilt, detectorBuilt)
	if err != nil {
		t.Fatal("target skin admission packet", err)
	}
	admit := func(body []byte, production bool) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				if production {
					return p.Reader.lockNativeTargetSkinAdmissionInTransaction(work, tx, who, original, plan, published.Graph.Built, parserBuilt, detectorBuilt)
				}
				if _, err := tx.Exec(work, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
					return err
				}
				var ready bool
				if err := tx.QueryRow(work, "SELECT photo.lock_semantic_target_skin_admission_evidence($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(body)).Scan(&ready); err != nil {
					return err
				}
				if ready {
					return v2input.ErrConflict
				}
				return nil
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	if err := admit(packet, true); err != nil {
		t.Fatal("target skin admission", err)
	}
	for _, key := range []string{"parser", "parser_result", "config_text", "params_pin", "registration"} {
		var changed map[string]any
		if err := json.Unmarshal(packet, &changed); err != nil {
			t.Fatal(err)
		}
		changed[key] = nil
		body, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := admit(body, false); err != nil {
			t.Fatal("target skin invalid admission", key, err)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied = admit(packet, true)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if denied == nil {
		t.Fatal("revoked target skin admission accepted")
	}
	if err := admit(packet, true); err != nil {
		t.Fatal("restored target skin admission", err)
	}
	t.Log("V27_TARGET_SKIN_ADMISSION_EVIDENCE_REVOKE_OK")

	seal := func() error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				ticket, err := p.Reader.sealNativeTargetSkinInTransaction(work, tx, who, original, plan, published.Graph.Built, parserBuilt, detectorBuilt)
				if err != nil {
					return err
				}
				if ticket.RunID != plan.shell.Run || ticket.WorkflowID != plan.shell.WorkflowID || ticket.State != "queued" {
					return v2input.ErrConflict
				}
				return nil
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := seal(); err != nil {
			t.Fatal("target skin atomic seal/replay", i, err)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied = seal()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if denied == nil {
		t.Fatal("revoked target skin sealed replay accepted")
	}
	if err := seal(); err != nil {
		t.Fatal("restored target skin sealed replay", err)
	}
	for _, query := range []string{"SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'", "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued'"} {
		var n int
		if err := owner.QueryRow(ctx, query, plan.shell.Tenant, plan.shell.Run).Scan(&n); err != nil || n != 1 {
			t.Fatal("target skin single queue event", n, err)
		}
	}
	t.Log("V27_TARGET_SKIN_ATOMIC_SEAL_REPLAY_REVOKE_OK")

	restore := func() error {
		a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, parser.detector.anchor.manifest, parser.detector.anchor.entry.Ordinal, "mask.face_skin")
		if err != nil {
			return err
		}
		saved, err := p.Reader.readNativeTargetSkinSaved(ctx, who, a)
		if err != nil {
			return err
		}
		restored, err := p.Reader.restoreNativeTargetSkinPlan(ctx, who, parser, a, p.Configuration.Color, saved)
		if err != nil {
			return err
		}
		if restored.seal != plan.seal {
			return v2input.ErrConflict
		}
		for _, kind := range []string{"config", "profile", "params"} {
			changed := saved
			switch kind {
			case "config":
				changed.ConfigSHA = "invalid"
			case "profile":
				changed.Profile = json.RawMessage(`{}`)
			case "params":
				changed.Params.ArtifactID = uuid.New()
			}
			if _, err := p.Reader.restoreNativeTargetSkinPlan(ctx, who, parser, a, p.Configuration.Color, changed); err == nil {
				return v2input.ErrConflict
			}
		}
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				built, err := p.Reader.recoverNativeTargetSkinTransport(work, tx, who, original, restored)
				if err != nil {
					return err
				}
				_, err = p.Reader.sealNativeTargetSkinInTransaction(work, tx, who, original, restored, built, parserBuilt, detectorBuilt)
				return err
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	if err := restore(); err != nil {
		t.Fatal("target skin queued saved restore", err)
	}
	t.Log("V27_TARGET_SKIN_SAVED_QUEUED_TRANSPORT_RESTORE_OK")

	probeNativeWireTaskOutputs(t, ctx, plan.shell.Tasks[0].ID)
	if err := seal(); err != nil {
		t.Fatal("target skin post-execution sealed replay", err)
	}
	t.Log("V27_TARGET_SKIN_CANONICAL_EXECUTION_REPLAY_OK")

	proof, err := p.Reader.readCompletedNativeTargetSkin(ctx, who, plan, p.Configuration.Color)
	if err != nil {
		t.Fatal("target skin physical read", err)
	}
	resultPacket, err := p.Reader.marshalNativeTargetSkinCompletedEvidence(ctx, who, original, plan, p.Configuration.Color, proof, published.Graph.Built, parserBuilt, detectorBuilt)
	if err != nil {
		t.Fatal("target skin physical packet", err)
	}
	lockResult := func(body []byte) error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.lockNativeCompletedTargetSkinInTransaction(work, tx, who, plan, body)
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	for i := 0; i < 2; i++ {
		if err := lockResult(resultPacket); err != nil {
			t.Fatal("target skin physical consumer", i, err)
		}
	}
	for _, key := range []string{"admission", "result_id", "outputs", "params", "files", "skin_pixels"} {
		var changed map[string]any
		if err := json.Unmarshal(resultPacket, &changed); err != nil {
			t.Fatal(err)
		}
		changed[key] = nil
		body, err := json.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if err := lockResult(body); err == nil {
			t.Fatal("target skin altered result accepted", key)
		}
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	denied = lockResult(resultPacket)
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if denied == nil {
		t.Fatal("target skin revoked result accepted")
	}
	if err := lockResult(resultPacket); err != nil {
		t.Fatal("restored target skin result", err)
	}
	t.Log("V27_TARGET_SKIN_PHYSICAL_RESULT_CONSUMER_REPLAY_REVOKE_OK")

	finish := func() error {
		ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.finishNativeTargetSkinInTransaction(work, tx, who, plan, resultPacket)
			})
		})
		if err != nil {
			return err
		}
		if !ok {
			return v2input.ErrConflict
		}
		return nil
	}
	var expiry time.Time
	var residual, beforeUsage int64
	if err := owner.QueryRow(ctx, "SELECT q.expires_at,q.bytes,u.reserved_bytes FROM photo.quota_reservations q JOIN photo.tenant_usage u ON u.tenant_id=q.tenant_id WHERE q.tenant_id=$1 AND q.holder_type='run' AND q.holder_id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&expiry, &residual, &beforeUsage); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", plan.shell.Tenant, plan.shell.Run); err != nil {
		t.Fatal(err)
	}
	expired := finish()
	if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=$3 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", plan.shell.Tenant, plan.shell.Run, expiry); err != nil {
		t.Fatal(err)
	}
	if expired == nil {
		t.Fatal("target skin expired quota finish accepted")
	}
	for i := 0; i < 2; i++ {
		if err := finish(); err != nil {
			t.Fatal("target skin finish/replay", i, err)
		}
	}
	var quotaState string
	var remaining, afterUsage int64
	if err := owner.QueryRow(ctx, "SELECT r.status,q.state,q.bytes,u.reserved_bytes FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id JOIN photo.tenant_usage u ON u.tenant_id=r.tenant_id WHERE r.tenant_id=$1 AND r.id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&status, &quotaState, &remaining, &afterUsage); err != nil || status != "completed" || quotaState != "released" || remaining != 0 || afterUsage != beforeUsage-residual {
		t.Fatal("target skin exact quota release", status, quotaState, remaining, beforeUsage, afterUsage, residual, err)
	}
	for _, query := range []string{"SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.completed'", "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.completed'"} {
		var n int
		if err := owner.QueryRow(ctx, query, plan.shell.Tenant, plan.shell.Run).Scan(&n); err != nil || n != 1 {
			t.Fatal("target skin one completion event", n, err)
		}
	}
	completed, err := p.Reader.readCompletedNativeTargetSkin(ctx, who, plan, p.Configuration.Color)
	if err != nil {
		t.Fatal("target skin completed physical read", err)
	}
	completedPacket, err := p.Reader.marshalNativeTargetSkinCompletedEvidence(ctx, who, original, plan, p.Configuration.Color, completed, published.Graph.Built, parserBuilt, detectorBuilt)
	if err != nil {
		t.Fatal("target skin completed packet", err)
	}
	if err := lockResult(completedPacket); err != nil {
		t.Fatal("target skin completed consumer", err)
	}
	if err := seal(); err != nil {
		t.Fatal("target skin completed sealed replay", err)
	}
	outputs, err := json.Marshal(completed.catalog.Outputs)
	if err != nil {
		t.Fatal(err)
	}
	var workerReady, consumerReady bool
	if err := owner.QueryRow(ctx, "SELECT photo.semantic_target_skin_result_ready(t.tenant_id,t.id,t.attempt,t.fencing_token,$2::jsonb),photo.semantic_target_skin_completed_output_ready(t.tenant_id,t.id,t.attempt,t.fencing_token,$2::jsonb) FROM photo.tasks t WHERE id=$1", plan.shell.Tasks[0].ID, string(outputs)).Scan(&workerReady, &consumerReady); err != nil || workerReady || !consumerReady {
		t.Fatal("target skin completed worker guard", workerReady, consumerReady, err)
	}
	t.Log("V27_TARGET_SKIN_FINISH_QUOTA_COMPLETED_REPLAY_OK")
	if err := restore(); err != nil {
		t.Fatal("target skin completed saved restore", err)
	}
	t.Log("V27_TARGET_SKIN_SAVED_COMPLETED_TRANSPORT_RESTORE_OK")

}
