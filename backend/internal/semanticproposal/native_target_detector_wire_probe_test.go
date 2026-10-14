package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Physical source/qualification and draft begin only. Target seal is separate.
func probeNativeTargetDetectorDraft(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, ordinal int) {
	t.Helper()
	a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest, ordinal, "face.detect")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference(ctx, who, d.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
	if err != nil {
		t.Fatal(err)
	}

	absent, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
	if err != nil || absent.State != "absent" {
		t.Fatal("target detector absent locator", err)
	}

	if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_ADVANCE") == "1" {
		factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
		advance := func(want string) error {
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
				out, err := p.advanceNativeTargetDetectors(work, who, d, manifest, factory)
				if err != nil {
					return err
				}
				if out.phase != nativePreparationWaiting || out.operation != want {
					return v2input.ErrConflict
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !ok {
				return v2input.ErrConflict
			}
			return nil
		}
		if err := advance("face.detect"); err != nil {
			t.Fatal("automatic target detector first begin/seal", err)
		}
		if err := advance("face.detect"); err != nil {
			t.Fatal("automatic target detector waiting restore", err)
		}
		saved, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
		if err != nil || !saved.Sealed {
			t.Fatal("automatic target detector saved", err)
		}
		probeNativeWireTaskOutputs(t, ctx, saved.Task)
		if err := advance("semantic.transfer"); err != nil {
			t.Fatal("automatic target detector result finish", err)
		}
		if err := advance("semantic.transfer"); err != nil {
			t.Fatal("automatic target detector completed repeat", err)
		}
		t.Log("V27_TARGET_DETECTOR_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK")
		return
	}
	recovery, err := nativePlanRecoveryFromTask(d.shell.Tasks[0], &d.prepared.qualification.report.ref, d.prepared.costs, d.shell.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	budget := nativePreparationBudget{Costs: recovery.Costs, RAM: recovery.RAM, VRAM: recovery.VRAM, Scratch: recovery.Scratch, Output: recovery.Output, Reserved: recovery.Reserved}
	plan, err := p.Reader.prepareNativeTargetDetectorPlan(ctx, catalog, p.Documents, who, a, p.Configuration.Color, d.prepared.qualification.report.ref, .8, 64, budget)
	if err != nil {
		t.Fatal("target detector physical preparation", err)
	}
	locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
		for i := 0; i < 2; i++ {
			if err := database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
				out, err := p.Reader.beginNativeTargetDetectorInTransaction(work, tx, who, d, plan, p.Configuration.Color)
				if err != nil {
					return err
				}
				want := "created"
				if i == 1 {
					want = "pending"
				}
				if out.RunID != plan.detector.shell.Run || out.Outcome != want {
					return v2input.ErrConflict
				}
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || !locked {
		t.Fatal("target detector blocked begin/replay", locked, err)
	}
	transport, _, err := publishNativeTargetDetectorTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: plan.detector.shell.Run})
	if err != nil {
		t.Fatal("target detector physical transport", err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		_, err := p.Reader.beginNativeTargetDetectorInTransaction(ctx, tx, who, d, plan, p.Configuration.Color)
		return err
	})
	if _, restoreErr := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Fatal("revoked target detector replay accepted")
	}

	if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_REGISTER") == "1" {
		register := func() error {
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					return p.Reader.registerNativeTargetDetectorInTransaction(work, tx, who, d, plan, p.Configuration.Color, transport.Graph.Built)
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
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		refused := register()
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if refused == nil {
			t.Fatal("revoked target detector registration accepted")
		}
		var count int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", d.request.TenantID, transport.Graph.ID).Scan(&count); err != nil || count != 0 {
			t.Fatal("revoked registration left partial rows", count, err)
		}
		if err := register(); err != nil {
			t.Fatal("target detector registration", err)
		}
		if err := register(); err != nil {
			t.Fatal("target detector registration replay", err)
		}
		t.Log("V27_TARGET_DETECTOR_PHYSICAL_REGISTRATION_REPLAY_REVOKE_OK")
		evidence, err := p.Reader.marshalNativeTargetDetectorAdmissionEvidence(ctx, who, d, plan, transport.Graph.Built)
		if err != nil {
			t.Fatal("target admission evidence", err)
		}
		locked, err = withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
			return database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
				if _, err := tx.Exec(lockCtx, "SELECT set_config('app.tenant_id',$1,true)", d.request.TenantID.String()); err != nil {
					return err
				}
				check := func(body []byte, want bool) error {
					var allowed bool
					if err := tx.QueryRow(lockCtx, "SELECT photo.lock_semantic_target_detector_admission_evidence($1,$2,$3::jsonb)", d.request.TenantID, plan.detector.shell.Run, string(body)).Scan(&allowed); err != nil {
						return err
					}
					if allowed != want {
						return fmt.Errorf("target admission allowed=%v want=%v", allowed, want)
					}
					return nil
				}
				if err := check(evidence, true); err != nil {
					return err
				}
				if err := check(evidence, true); err != nil {
					return err
				}
				for _, field := range []string{"binding", "request_sha", "anchor", "params_document_id", "policy_text", "registration", "qualification"} {
					var packet map[string]any
					if err := json.Unmarshal(evidence, &packet); err != nil {
						return err
					}
					target := packet["target"].(map[string]any)
					target[field] = nil
					changed, err := json.Marshal(packet)
					if err != nil {
						return err
					}
					if err := check(changed, false); err != nil {
						return fmt.Errorf("target admission substitution %s: %w", field, err)
					}
				}
				return nil
			})
		})
		if err != nil || !locked {
			t.Fatal("physical target admission authority", locked, err)
		}
		t.Log("V27_TARGET_DETECTOR_PHYSICAL_ADMISSION_EVIDENCE_OK")

		if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_SEAL") == "1" {
			invoke := func(want bool) error {
				locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
					return database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
						ticket, err := p.Reader.sealNativeTargetDetectorInTransaction(lockCtx, tx, who, d, plan, transport.Graph.Built)
						if !want {
							if err == nil || ticket != (v2input.JobTicket{}) {
								return fmt.Errorf("revoked target seal accepted")
							}
							return nil
						}
						if err != nil {
							return err
						}
						if ticket.RunID != plan.detector.shell.Run || ticket.WorkflowID != plan.detector.shell.WorkflowID || ticket.State != "queued" {
							return v2input.ErrConflict
						}
						return nil
					})
				})
				if err != nil {
					return err
				}
				if !locked {
					return fmt.Errorf("target seal parent lock unavailable")
				}
				return nil
			}
			// Current authorization is checked before first activation and on sealed replay.
			for _, want := range []bool{false, true, true, false, true} {
				if want {
					_, err = owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID)
				} else {
					_, err = owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID)
				}
				if err != nil {
					t.Fatal(err)
				}
				if err := invoke(want); err != nil {
					t.Fatal("target atomic seal/replay", want, err)
				}
			}
			t.Log("V27_TARGET_DETECTOR_PHYSICAL_ATOMIC_SEAL_REPLAY_REVOKE_OK")
			restore := func() error {
				saved, err := p.Reader.readNativeTargetDetectorSaved(ctx, who, a)
				if err != nil {
					return err
				}
				restored, err := p.Reader.restoreNativeTargetDetectorPlan(ctx, catalog, who, a, p.Configuration.Color, saved)
				if err != nil {
					return err
				}
				if restored.seal != plan.seal {
					return v2input.ErrConflict
				}
				ok, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
					return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
						built, err := p.Reader.recoverNativeTargetDetectorTransport(work, tx, who, d, restored)
						if err != nil {
							return err
						}
						ticket, err := p.Reader.sealNativeTargetDetectorInTransaction(work, tx, who, d, restored, built)
						if err != nil {
							return err
						}
						if ticket.RunID != plan.detector.shell.Run {
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
			if err := restore(); err != nil {
				t.Fatal("queued target detector restore", err)
			}

			if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_EXECUTE") == "1" {
				if probeNativeTargetFailureIfRequested(t, ctx, p, who, d, plan.detector.shell.Run, plan.detector.shell.Tasks[0].ID, "face.detect") {
					return
				}
				probeNativeWireTaskOutputs(t, ctx, plan.detector.shell.Tasks[0].ID)
				var status, state string
				var canonical bool
				if err := owner.QueryRow(ctx, "SELECT r.status,t.state,t.canonical_result_id IS NOT NULL FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1", plan.detector.shell.Tasks[0].ID).Scan(&status, &state, &canonical); err != nil || status != "running" || state != "succeeded" || !canonical {
					t.Fatal("target detector execution state", status, state, canonical, err)
				}
				if err := invoke(true); err != nil {
					t.Fatal("target detector sealed replay after canonical commit", err)
				}

				proof, err := p.Reader.readNativeTargetDetectorResult(ctx, who, plan, p.Configuration.Color)
				if err != nil {
					t.Fatal("target detector physical result", err)
				}
				again, err := p.Reader.readNativeTargetDetectorResult(ctx, who, plan, p.Configuration.Color)
				if err != nil || again.seal != proof.seal {
					t.Fatal("target detector result repeat", err)
				}
				lockProof := func(value nativeTargetDetectorResult) error {
					ok, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
						return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
							return p.Reader.lockNativeTargetDetectorResult(work, tx, who, d, plan, transport.Graph.Built, value)
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
				if err := lockProof(proof); err != nil {
					t.Fatal("target detector consumer lock", err)
				}
				for _, kind := range []string{"result", "output", "body", "object"} {
					bad := proof
					switch kind {
					case "result":
						bad.completed.catalog.ResultID = uuid.New()
					case "output":
						bad.completed.catalog.Output = plan.anchor.entry.Binding.Source
					case "body":
						bad.completed.body = "{}"
					case "object":
						bad.completed.objects[0].object.VersionID = "substituted"
					}
					if kind == "result" || kind == "output" {
						bad.seal, _ = targetDetectorResultDigest(bad)
					}
					if err := lockProof(bad); err == nil {
						t.Fatal("target detector substituted proof accepted", kind)
					}
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				_, readErr := p.Reader.readNativeTargetDetectorResult(ctx, who, plan, p.Configuration.Color)
				lockErr := lockProof(proof)
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				if readErr == nil || lockErr == nil {
					t.Fatal("revoked target detector result authority accepted")
				}
				if err := lockProof(proof); err != nil {
					t.Fatal("restored target detector result lock", err)
				}
				t.Log("V27_TARGET_DETECTOR_PHYSICAL_RESULT_READ_LOCK_REPLAY_REVOKE_OK")
				if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_FINISH") == "1" {
					finish := func() error {
						ok, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(work context.Context) error {
							return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
								return p.Reader.finishNativeTargetDetectorInTransaction(work, tx, who, d, plan, transport.Graph.Built, proof)
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
					if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", d.request.TenantID, plan.detector.shell.Run); err != nil {
						t.Fatal(err)
					}
					if err := finish(); err == nil {
						t.Fatal("expired target detector reservation finished")
					}
					if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '1 hour' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", d.request.TenantID, plan.detector.shell.Run); err != nil {
						t.Fatal(err)
					}
					var before, residual, after int64
					if err := owner.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", d.request.TenantID).Scan(&before); err != nil {
						t.Fatal(err)
					}
					if err := owner.QueryRow(ctx, "SELECT bytes FROM photo.quota_reservations WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", d.request.TenantID, plan.detector.shell.Run).Scan(&residual); err != nil {
						t.Fatal(err)
					}
					if err := finish(); err != nil {
						t.Fatal("target detector finish", err)
					}
					if err := finish(); err != nil {
						t.Fatal("target detector finish replay", err)
					}
					if err := owner.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", d.request.TenantID).Scan(&after); err != nil || after != before-residual {
						t.Fatal("target detector exact quota release", before, residual, after, err)
					}
					completed, err := p.Reader.readNativeTargetDetectorResult(ctx, who, plan, p.Configuration.Color)
					if err != nil || completed.seal != proof.seal {
						t.Fatal("completed detector physical reader", err)
					}
					if err := lockProof(completed); err != nil {
						t.Fatal("completed detector consumer lock", err)
					}
					if err := invoke(true); err != nil {
						t.Fatal("completed detector seal replay", err)
					}
					if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
						t.Fatal(err)
					}
					refused := finish()
					if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
						t.Fatal(err)
					}
					if refused == nil {
						t.Fatal("revoked detector finish replay accepted")
					}
					if err := finish(); err != nil {
						t.Fatal("restored detector finish replay", err)
					}
					if err := restore(); err != nil {
						t.Fatal("completed target detector restore", err)
					}
					t.Log("V27_TARGET_DETECTOR_SAVED_QUEUED_COMPLETED_TRANSPORT_RESTORE_OK")
					if os.Getenv("PHOTO_TEST_V27_TARGET_FACE_PLAN") == "1" {
						probeNativeTargetFacePlans(t, ctx, p, who, d, plan, transport.Graph.Built, owner)
					}
					t.Log("V27_TARGET_DETECTOR_PHYSICAL_FINISH_QUOTA_COMPLETED_REPLAY_OK")
				}

				t.Log("V27_TARGET_DETECTOR_PHYSICAL_CANONICAL_EXECUTION_REPLAY_OK")
			}

		}

	}
	t.Log("V27_TARGET_DETECTOR_PHYSICAL_BLOCKED_BEGIN_REPLAY_REVOKE_OK")
}
