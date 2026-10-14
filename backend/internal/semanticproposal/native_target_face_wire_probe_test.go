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

// Physical factories/compiler only; no target face draft persistence or seal.
func probeNativeTargetFacePlans(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, detector nativeTargetDetectorPlan, detectorBuilt v2input.Built, owner *pgxpool.Pool) {
	t.Helper()

	if os.Getenv("PHOTO_TEST_V27_TARGET_FACE_ADVANCE") == "1" {
		factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
		advance := func(want string) {
			t.Helper()
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				out, err := p.advanceNativeTargetFaces(work, who, original, detector.anchor.manifest, factory)
				if err != nil {
					return err
				}
				if out.phase != nativePreparationWaiting || out.operation != want {
					return v2input.ErrConflict
				}
				return nil
			})
			if err != nil || !ok {
				t.Fatal("automatic target face advance", want, ok, err)
			}
		}
		for _, op := range []string{"face.landmarks", "face.parse"} {
			advance(op)
			advance(op)
			a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, detector.anchor.manifest, detector.anchor.entry.Ordinal, op)
			if err != nil {
				t.Fatal(err)
			}
			saved, err := p.Reader.readNativeTargetFaceSaved(ctx, who, a)
			if err != nil || !saved.Sealed {
				t.Fatal("automatic target face saved", op, err)
			}
			probeNativeWireTaskOutputs(t, ctx, saved.Task)
		}
		advance("semantic.transfer")
		advance("semantic.transfer")
		t.Log("V27_TARGET_FACE_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK")
		return
	}
	catalog, err := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference(ctx, who, detector.anchor.request.TenantID, p.Reader.ProjectID, detector.anchor.entry.Binding.AssetVersionID)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := p.Reader.selectInitialFacePipelineConfiguration(ctx, who, detector.anchor.request.TenantID, p.Configuration, p.ReservedBytes, p.TransportDocuments)
	if err != nil {
		t.Fatal(err)
	}
	result, err := p.Reader.readNativeTargetDetectorResult(ctx, who, detector, p.Configuration.Color)
	if err != nil {
		t.Fatal(err)
	}
	for _, op := range []string{"face.landmarks", "face.parse"} {
		stage := cfg.Landmarks
		if op == "face.parse" {
			stage = cfg.Parser
		}
		params, err := p.Reader.publishNativeTargetFaceStageParams(ctx, who, detector, p.Configuration.Color, p.Documents, op)
		if err != nil {
			t.Fatal("target face params", op, err)
		}
		task, err := p.Reader.prepareNativeTargetFaceTask(ctx, catalog, who, detector, p.Configuration.Color, params, stage.Report, stage.Budget)
		if err != nil {
			t.Fatal("target face task", op, err)
		}
		plan, err := p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, task, stage.Budget.Reserved)
		if err != nil {
			t.Fatal("target face compiler", op, err)
		}
		var queueConfig map[string]json.RawMessage
		if json.Unmarshal(plan.shell.Config, &queueConfig) != nil || string(queueConfig["queue_class"]) != `"batch"` { t.Fatal("target face queue missing", op) }
		oldTask := task
		oldTask.legacyQueueOmitted = true
		oldPlan, err := p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, oldTask, stage.Budget.Reserved)
		if err != nil { t.Fatal("historical target face compile", op, err) }
		var oldConfig map[string]json.RawMessage
		if json.Unmarshal(oldPlan.shell.Config, &oldConfig) != nil { t.Fatal("historical target face config", op) }
		if _, present := oldConfig["queue_class"]; present { t.Fatal("historical target face queue fabricated", op) }
		oldReplay, err := p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, oldPlan.prepared, stage.Budget.Reserved)
		if err != nil || oldReplay.seal != oldPlan.seal || string(oldReplay.shell.Config) != string(oldPlan.shell.Config) { t.Fatal("historical target face replay changed", op, err) }
		if err := verifyNativeTargetFacePlanSeal(ctx, plan); err != nil {
			t.Fatal(err)
		}
		again, err := p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, result, task, stage.Budget.Reserved)
		if err != nil || again.seal != plan.seal {
			t.Fatal("target face deterministic replay", op, err)
		}
		for _, kind := range []string{"namespace", "result", "params", "actor", "operation", "report", "budget", "fingerprint"} {
			changed := task
			proof := result
			reserved := stage.Budget.Reserved
			switch kind {
			case "namespace":
				changed.parent = detector.anchor.request.RunID
			case "result":
				proof.completed.catalog.ResultID = uuid.New()
				proof.seal, _ = targetDetectorResultDigest(proof)
			case "params":
				changed.document = uuid.New()
			case "actor":
				changed.actor = uuid.New()
			case "operation":
				changed.operation = "mask.person"
			case "report":
				changed.report.ref.ArtifactID = uuid.Nil
			case "budget":
				reserved = 1
			case "fingerprint":
				proof.completed.body = "{}"
			}
			if _, err := p.Reader.compileNativeTargetFaceDraftPlan(ctx, who, detector, proof, changed, reserved); err == nil {
				t.Fatal("target face substitution accepted", op, kind)
			}
		}
		transport, _, err := publishNativeTargetFaceDraftTransport(ctx, plan, &proposalMemoryTransport{})
		if err != nil || transport.Graph.ID != plan.shell.Tasks[0].ID {
			t.Fatal("target face memory transport", op, err)
		}
		t.Log("V27_TARGET_FACE_PHYSICAL_FACTORY_COMPILER_OK:" + op)
		for _, outcome := range []string{"created", "pending"} {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					admission, err := p.Reader.beginNativeTargetFaceDraftInTransaction(work, tx, who, original, plan, p.Configuration.Color, detectorBuilt)
					if err != nil {
						return err
					}
					if admission.RunID != plan.shell.Run || admission.Outcome != outcome {
						return v2input.ErrConflict
					}
					return nil
				})
			})
			if err != nil || !locked {
				t.Fatal("target face blocked draft/replay", op, outcome, err)
			}
		}
		t.Log("V27_TARGET_FACE_BLOCKED_DRAFT_REPLAY_OK:" + op)
		published, _, err := publishNativeTargetFaceDraftTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: plan.shell.Tenant, Run: plan.shell.Run})
		if err != nil {
			t.Fatal("target face physical transport", op, err)
		}
		for i := 0; i < 2; i++ {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					return p.Reader.registerNativeTargetFaceInputsInTransaction(work, tx, who, original, plan, p.Configuration.Color, detectorBuilt, published.Graph.Built)
				})
			})
			if err != nil || !locked {
				t.Fatal("target face registration/replay", op, i, err)
			}
		}
		t.Log("V27_TARGET_FACE_REGISTERED_REPLAY_OK:" + op)
		packet, err := p.Reader.marshalNativeTargetFaceAdmissionEvidence(ctx, who, original, plan, published.Graph.Built, detectorBuilt)
		if err != nil {
			t.Fatal("target face admission packet", op, err)
		}
		invokeAdmission := func(body []byte) error {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					if _, err := tx.Exec(work, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
						return err
					}
					var ready bool
					if err := tx.QueryRow(work, "SELECT photo.lock_semantic_target_face_admission_evidence($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(body)).Scan(&ready); err != nil {
						return err
					}
					if !ready {
						return v2input.ErrConflict
					}
					return nil
				})
			})
			if err != nil {
				return err
			}
			if !locked {
				return v2input.ErrConflict
			}
			return nil
		}
		if err := invokeAdmission(packet); err != nil {
			t.Fatal("target face admission", op, err)
		}
		for _, field := range []string{"actor_id", "run_id", "parent_run_id", "request_sha", "policy_text", "binding", "expected", "params_pin", "files", "registration"} {
			var changed map[string]any
			if err := json.Unmarshal(packet, &changed); err != nil {
				t.Fatal(err)
			}
			face := changed["face"].(map[string]any)
			face[field] = nil
			bad, err := json.Marshal(changed)
			if err != nil {
				t.Fatal(err)
			}
			if err := invokeAdmission(bad); err == nil {
				t.Fatal("target face admission substitution accepted", op, field)
			}
		}
		t.Log("V27_TARGET_FACE_ADMISSION_PROOF_NEGATIVES_OK:" + op)

		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		_, denied := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
			return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.registerNativeTargetFaceInputsInTransaction(work, tx, who, original, plan, p.Configuration.Color, detectorBuilt, published.Graph.Built)
			})
		})
		admissionDenied := invokeAdmission(packet)
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if denied == nil || admissionDenied == nil {
			t.Fatal("target face registration accepted revoked session", op)
		}
		t.Log("V27_TARGET_FACE_REGISTRATION_REVOKE_OK:" + op)
		seal := func() error {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					ticket, err := p.Reader.sealNativeTargetFaceInTransaction(work, tx, who, original, plan, published.Graph.Built, detectorBuilt)
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
			if !locked {
				return v2input.ErrConflict
			}
			return nil
		}
		for i := 0; i < 2; i++ {
			if err := seal(); err != nil {
				t.Fatal("target face atomic seal/replay", op, i, err)
			}
		}
		var status, state string
		var sealed bool
		if err := owner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.id=$1", plan.shell.Run).Scan(&status, &sealed, &state); err != nil || status != "queued" || !sealed || state != "ready" {
			t.Fatal("target face sealed state", op, status, state, sealed, err)
		}
		for _, query := range []string{"SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.queued'", "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.queued'"} {
			var count int
			if err := owner.QueryRow(ctx, query, plan.shell.Tenant, plan.shell.Run).Scan(&count); err != nil || count != 1 {
				t.Fatal("target face exactly one queue event", op, count, err)
			}
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		sealDenied := seal()
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if sealDenied == nil {
			t.Fatal("target face sealed replay accepted revoked session", op)
		}
		if err := seal(); err != nil {
			t.Fatal("target face sealed restore replay", op, err)
		}
		t.Log("V27_TARGET_FACE_ATOMIC_SEAL_REPLAY_REVOKE_OK:" + op)
		restore := func() error {
			a, err := compileNativeTargetAnchor(ctx, who, original.request, original.requestKey, original.published, original.anchor.snapshotRef, original.anchor.snapshotBody, detector.anchor.manifest, detector.anchor.entry.Ordinal, op)
			if err != nil {
				return err
			}
			saved, err := p.Reader.readNativeTargetFaceSaved(ctx, who, a)
			if err != nil {
				return err
			}
			restored, err := p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, detector, a, p.Configuration.Color, saved)
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
				if _, err := p.Reader.restoreNativeTargetFacePlan(ctx, catalog, who, detector, a, p.Configuration.Color, changed); err == nil {
					t.Fatal("target face saved substitution accepted", op, kind)
				}
			}
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					built, err := p.Reader.recoverNativeTargetFaceTransport(work, tx, who, original, restored)
					if err != nil {
						return err
					}
					_, err = p.Reader.sealNativeTargetFaceInTransaction(work, tx, who, original, restored, built, detectorBuilt)
					return err
				})
			})
			if err != nil {
				return err
			}
			if !locked {
				return v2input.ErrConflict
			}
			return nil
		}
		if err := restore(); err != nil {
			t.Fatal("target face queued restore", op, err)
		}
		t.Log("V27_TARGET_FACE_SAVED_QUEUED_TRANSPORT_RESTORE_OK:" + op)

		if os.Getenv("PHOTO_TEST_V27_TARGET_FACE_EXECUTE") == "1" {
			if probeNativeTargetFailureIfRequested(t, ctx, p, who, original, plan.shell.Run, plan.shell.Tasks[0].ID, op) {
				return
			}
			probeNativeWireTaskOutputs(t, ctx, plan.shell.Tasks[0].ID)
			var canonical bool
			if err := owner.QueryRow(ctx, "SELECT r.status,t.state,t.canonical_result_id IS NOT NULL FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1", plan.shell.Tasks[0].ID).Scan(&status, &state, &canonical); err != nil || status != "running" || state != "succeeded" || !canonical {
				t.Fatal("target face canonical execution", op, status, state, canonical, err)
			}
			if err := seal(); err != nil {
				t.Fatal("target face sealed replay after canonical commit", op, err)
			}
			t.Log("V27_TARGET_FACE_CANONICAL_EXECUTION_REPLAY_OK:" + op)
			proof, err := p.Reader.readNativeTargetFaceResult(ctx, who, plan, p.Configuration.Color)
			if err != nil {
				t.Fatal("target face physical result", op, err)
			}
			again, err := p.Reader.readNativeTargetFaceResult(ctx, who, plan, p.Configuration.Color)
			if err != nil || again.seal != proof.seal {
				t.Fatal("target face physical repeat", op, err)
			}
			lockResult := func(value nativeTargetFaceResult) error {
				locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
					return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
						return p.Reader.lockNativeTargetFaceResult(work, tx, who, original, plan, published.Graph.Built, detectorBuilt, value)
					})
				})
				if err != nil {
					return err
				}
				if !locked {
					return v2input.ErrConflict
				}
				return nil
			}
			if err := lockResult(proof); err != nil {
				t.Fatal("target face result lock", op, err)
			}
			for _, kind := range []string{"fingerprint", "plan", "result", "output"} {
				changed := proof
				changed.catalog.Outputs = append([]v2input.Ref(nil), proof.catalog.Outputs...)
				switch kind {
				case "fingerprint":
					changed.body = "{}"
				case "plan":
					changed.planSeal = "invalid"
				case "result":
					changed.catalog.ResultID = uuid.New()
					changed.seal, _ = targetFaceResultDigest(changed)
				case "output":
					changed.catalog.Outputs[0].ArtifactID = uuid.New()
					changed.seal, _ = targetFaceResultDigest(changed)
				}
				if err := lockResult(changed); err == nil {
					t.Fatal("target face result substitution accepted", op, kind)
				}
			}
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			_, readDenied := p.Reader.readNativeTargetFaceResult(ctx, who, plan, p.Configuration.Color)
			lockDenied := lockResult(proof)
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			if readDenied == nil || lockDenied == nil {
				t.Fatal("target face result accepted revoked session", op)
			}
			if err := lockResult(proof); err != nil {
				t.Fatal("target face result lock restored", op, err)
			}
			t.Log("V27_TARGET_FACE_PHYSICAL_RESULT_READ_LOCK_REPLAY_REVOKE_OK:" + op)
			if os.Getenv("PHOTO_TEST_V27_TARGET_FACE_FINISH") == "1" {
				finish := func() error {
					locked, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
						return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
							return p.Reader.finishNativeTargetFaceInTransaction(work, tx, who, original, plan, published.Graph.Built, detectorBuilt, proof)
						})
					})
					if err != nil {
						return err
					}
					if !locked {
						return v2input.ErrConflict
					}
					return nil
				}
				var expiry time.Time
				var residual, reservedBefore int64
				if err := owner.QueryRow(ctx, "SELECT q.expires_at,q.bytes,u.reserved_bytes FROM photo.quota_reservations q JOIN photo.tenant_usage u ON u.tenant_id=q.tenant_id WHERE q.tenant_id=$1 AND q.holder_type='run' AND q.holder_id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&expiry, &residual, &reservedBefore); err != nil {
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
					t.Fatal("target face finish accepted expired quota", op)
				}
				for i := 0; i < 2; i++ {
					if err := finish(); err != nil {
						t.Fatal("target face finish/replay", op, i, err)
					}
				}
				var quotaState string
				var remaining, reservedAfter int64
				if err := owner.QueryRow(ctx, "SELECT r.status,q.state,q.bytes,u.reserved_bytes FROM photo.runs r JOIN photo.quota_reservations q ON q.tenant_id=r.tenant_id AND q.holder_type='run' AND q.holder_id=r.id JOIN photo.tenant_usage u ON u.tenant_id=r.tenant_id WHERE r.tenant_id=$1 AND r.id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&status, &quotaState, &remaining, &reservedAfter); err != nil || status != "completed" || quotaState != "released" || remaining != 0 || reservedAfter != reservedBefore-residual {
					t.Fatal("target face quota exact release", op, status, quotaState, remaining, reservedBefore, reservedAfter, residual, err)
				}
				for _, query := range []string{"SELECT count(*) FROM photo.run_events WHERE tenant_id=$1 AND run_id=$2 AND type='v2.run.completed'", "SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$2::text AND routing_key='v2.run.completed'"} {
					var count int
					if err := owner.QueryRow(ctx, query, plan.shell.Tenant, plan.shell.Run).Scan(&count); err != nil || count != 1 {
						t.Fatal("target face one completion event", op, count, err)
					}
				}
				completed, err := p.Reader.readNativeTargetFaceResult(ctx, who, plan, p.Configuration.Color)
				if err != nil || completed.seal != proof.seal {
					t.Fatal("target face completed physical read", op, err)
				}
				if err := lockResult(completed); err != nil {
					t.Fatal("target face completed consumer", op, err)
				}
				if err := seal(); err != nil {
					t.Fatal("target face completed sealed replay", op, err)
				}
				outputs, err := json.Marshal(proof.catalog.Outputs)
				if err != nil {
					t.Fatal(err)
				}
				var workerReady, consumerReady bool
				if err := owner.QueryRow(ctx, "SELECT photo.semantic_target_face_result_ready(t.tenant_id,t.id,t.attempt,t.fencing_token,$2::jsonb),photo.semantic_target_face_completed_output_ready(t.tenant_id,t.id,t.attempt,t.fencing_token,$2::jsonb) FROM photo.tasks t WHERE id=$1", plan.shell.Tasks[0].ID, string(outputs)).Scan(&workerReady, &consumerReady); err != nil || workerReady || !consumerReady {
					t.Fatal("target face completed worker guard", op, workerReady, consumerReady, err)
				}
				t.Log("V27_TARGET_FACE_FINISH_QUOTA_COMPLETED_REPLAY_OK:" + op)
				if err := restore(); err != nil {
					t.Fatal("target face completed restore", op, err)
				}
				t.Log("V27_TARGET_FACE_SAVED_COMPLETED_TRANSPORT_RESTORE_OK:" + op)
				if op == "face.parse" && os.Getenv("PHOTO_TEST_V27_TARGET_SKIN_PLAN") == "1" {
					probeNativeTargetSkinPlan(t, ctx, p, who, original, plan, published.Graph.Built, detectorBuilt, owner)
				}
			}

		}

	}
}
