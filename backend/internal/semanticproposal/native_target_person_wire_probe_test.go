package semanticproposal

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

func probeNativeTargetPersonDraft(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, d detectorDraftPlan, manifest nativeTargetPreparationManifest, report v2input.Ref) {
	t.Helper()
	ordinal := -1
	for i, binding := range d.published.Inventory.Targets {
		if binding != d.request.Intent.Reference {
			ordinal = i
			break
		}
	}
	if ordinal < 0 {
		t.Fatal("physical target fixture has no distinct target")
	}
	a, err := compileNativeTargetAnchor(ctx, who, d.request, d.requestKey, d.published, d.anchor.snapshotRef, d.anchor.snapshotBody, manifest, ordinal, "mask.person")
	if err != nil {
		t.Fatal(err)
	}
	if os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_ADVANCE") == "1" {
		factory := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference
		advance := func(want string) error {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
				out, err := p.advanceNativeTargetPersons(lockCtx, who, d, manifest, factory)
				if err != nil {
					return err
				}
				if out.phase != nativePreparationWaiting || out.operation != want {
					return fmt.Errorf("target advance phase=%s operation=%s", out.phase, out.operation)
				}
				return nil
			})
			if err != nil {
				return err
			}
			if !locked {
				return fmt.Errorf("target advance parent lock unavailable")
			}
			return nil
		}
		absent, err := p.Reader.readNativeTargetPersonSaved(ctx, who, a)
		if err != nil || absent.State != "absent" {
			t.Fatal("target absent recovery", err)
		}
		if err := advance("mask.person"); err != nil {
			t.Fatal("target automatic begin/publication/registration/seal", err)
		}
		if err := advance("mask.person"); err != nil {
			t.Fatal("target automatic waiting restore", err)
		}
		saved, err := p.Reader.readNativeTargetPersonSaved(ctx, who, a)
		if err != nil || saved.State != "saved" || !saved.Sealed {
			t.Fatal("target saved recovery", err)
		}

		if probeNativeTargetFailureIfRequested(t, ctx, p, who, d, saved.Run, saved.Task, "mask.person") {
			return
		}
		probeNativeWireTaskOutputs(t, ctx, saved.Task)
		if err := advance("semantic.transfer"); err != nil {
			t.Fatal("target automatic canonical finish", err)
		}
		if err := advance("semantic.transfer"); err != nil {
			t.Fatal("target automatic completed restore replay", err)
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DETECTOR_DRAFT") == "1" {
			probeNativeTargetDetectorDraft(t, ctx, p, who, d, manifest, ordinal)
		}
		t.Log("V27_TARGET_PERSON_AUTOMATIC_ADVANCE_RESTORE_FINISH_OK")
		return
	}

	catalog, err := (SQLNativeParentArtifactCatalogFactory{API: p.Reader.DB, Bucket: p.Reader.Bucket}).ForReference(ctx, who, d.request.TenantID, p.Reader.ProjectID, a.entry.Binding.AssetVersionID)
	if err != nil {
		t.Fatal(err)
	}
	selected, err := p.Reader.selectNativePersonProtectionPolicy(ctx, who, d.request.TenantID, p.Configuration.MetadataBytes)
	if err != nil {
		t.Fatal(err)
	}
	if selected.Report != report {
		t.Fatal("target person report changed")
	}
	pin, err := p.Reader.personQualificationObject(ctx, who, d.request.TenantID, selected.Expected, report)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := v2input.ReadPinnedSemanticDocument(ctx, p.Reader.Objects, pin.object, report)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := v2qualification.Verify(raw, report.SHA256, selected.Expected)
	if err != nil {
		t.Fatal(err)
	}
	budget, err := createPersonBudget(p.Configuration, p.ReservedBytes, verified.Metrics)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := p.Reader.prepareNativeTargetPersonPlan(ctx, catalog, p.Documents, who, a, p.Configuration.Color, report, 512, budget)
	if err != nil {
		t.Fatal("physical target person preparation", err)
	}
	locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
		for i := 0; i < 2; i++ {
			err := database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
				admitted, err := p.Reader.beginNativeTargetPersonInTransaction(lockCtx, tx, who, d, plan, p.Configuration.Color)
				if err != nil {
					return err
				}
				expected := "created"
				if i == 1 {
					expected = "pending"
				}
				if admitted.RunID != plan.person.shell.Run || admitted.Outcome != expected {
					return v2input.ErrConflict
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || !locked {
		t.Fatal("physical target person blocked admission/replay", locked, err)
	}
	owner, err := pgxpool.New(ctx, os.Getenv("PHOTO_TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	defer owner.Close()

	transport, _, err := publishNativeTargetPersonTransport(ctx, plan, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: d.request.TenantID, Run: plan.person.shell.Run})
	if err != nil {
		t.Fatal("physical target transport publication", err)
	}
	// Revocation after publication must not register even a subset of inputs.
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	err = database.InTransaction(ctx, p.Scheduler, func(tx pgx.Tx) error {
		return p.Reader.registerNativeTargetPersonInTransaction(ctx, tx, who, d, plan, p.Configuration.Color, transport.Graph.Built)
	})
	if err == nil {
		t.Fatal("revoked target transport registered")
	}
	var registered int
	if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", d.request.TenantID, transport.Graph.ID).Scan(&registered); err != nil || registered != 0 {
		t.Fatal("revoked registration left partial target inputs", registered, err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	locked, err = withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
		for i := 0; i < 2; i++ {
			if err := database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
				return p.Reader.registerNativeTargetPersonInTransaction(lockCtx, tx, who, d, plan, p.Configuration.Color, transport.Graph.Built)
			}); err != nil {
				return fmt.Errorf("registration iteration %d: %w", i, err)
			}
		}
		return nil
	})
	if err != nil || !locked {
		t.Fatal("physical target transport registration/replay", locked, err)
	}
	evidence, err := p.Reader.marshalNativeTargetPersonAdmissionEvidence(ctx, who, d, plan, transport.Graph.Built)
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
				if err := tx.QueryRow(lockCtx, "SELECT photo.lock_semantic_target_person_admission_evidence($1,$2,$3::jsonb)", d.request.TenantID, plan.person.shell.Run, string(body)).Scan(&allowed); err != nil {
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
	t.Log("V27_TARGET_PERSON_PHYSICAL_ADMISSION_EVIDENCE_OK")

	t.Log("V27_TARGET_PERSON_PHYSICAL_TRANSPORT_REPLAY_REVOKE_OK")
	var status, state string
	var sealed, canonical bool
	task := plan.person.shell.Tasks[0]
	if err := owner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state,t.canonical_result_id IS NOT NULL FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2 AND t.id=$3", d.request.TenantID, plan.person.shell.Run, task.ID).Scan(&status, &sealed, &state, &canonical); err != nil {
		t.Fatal(err)
	}
	if status != "draft" || sealed || state != "blocked" || canonical {
		t.Fatal("target preparation activated without dedicated seal", status, sealed, state, canonical)
	}
	t.Log("V27_TARGET_PERSON_PHYSICAL_BLOCKED_DRAFT_REPLAY_OK")
	if os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_SEAL") == "1" {
		invoke := func(want bool) error {
			locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
				return database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
					ticket, err := p.Reader.sealNativeTargetPersonInTransaction(lockCtx, tx, who, d, plan, transport.Graph.Built)
					if !want {
						if err == nil || ticket != (v2input.JobTicket{}) {
							return fmt.Errorf("revoked target seal accepted")
						}
						return nil
					}
					if err != nil {
						return err
					}
					if ticket.RunID != plan.person.shell.Run || ticket.WorkflowID != plan.person.shell.WorkflowID || ticket.State != "queued" {
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
		t.Log("V27_TARGET_PERSON_PHYSICAL_ATOMIC_SEAL_REPLAY_REVOKE_OK")
		if os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_EXECUTE") == "1" {
			probeNativeWireTaskOutputs(t, ctx, task.ID)
			var result bool
			if err := owner.QueryRow(ctx, "SELECT r.status,t.state,t.canonical_result_id IS NOT NULL FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE t.id=$1", task.ID).Scan(&status, &state, &result); err != nil || status != "running" || state != "succeeded" || !result {
				t.Fatal("target execution state", status, state, result, err)
			}
			if err := invoke(true); err != nil {
				t.Fatal("target sealed replay after canonical commit", err)
			}
			proof, err := p.Reader.readNativeTargetPersonResult(ctx, who, plan)
			if err != nil {
				t.Fatal("target physical result reader", err)
			}
			again, err := p.Reader.readNativeTargetPersonResult(ctx, who, plan)
			if err != nil || again.seal != proof.seal {
				t.Fatal("target physical result read replay", err)
			}
			lockProof := func(value nativeTargetPersonResult) error {
				locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
					return database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
						return p.Reader.lockNativeTargetPersonResult(lockCtx, tx, who, d, plan, transport.Graph.Built, value)
					})
				})
				if err != nil {
					return err
				}
				if !locked {
					return fmt.Errorf("target result parent lock unavailable")
				}
				return nil
			}
			if err := lockProof(proof); err != nil {
				t.Fatal("target result authority", err)
			}
			if err := lockProof(proof); err != nil {
				t.Fatal("target result authority replay", err)
			}
			bad := proof
			bad.result = uuid.New()
			bad.seal, err = targetPersonResultDigest(bad)
			if err != nil {
				t.Fatal(err)
			}
			if err := lockProof(bad); err == nil {
				t.Fatal("foreign canonical result adopted")
			}
			bad = proof
			bad.outputs = append([]v2input.Ref(nil), proof.outputs...)
			bad.outputs[3].ArtifactID = uuid.New()
			bad.seal, err = targetPersonResultDigest(bad)
			if err != nil {
				t.Fatal(err)
			}
			if err := lockProof(bad); err == nil {
				t.Fatal("foreign person report adopted")
			}

			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := lockProof(proof); err == nil {
				t.Fatal("revoked target result adopted")
			}
			if denied, err := p.Reader.readNativeTargetPersonResult(ctx, who, plan); err == nil || denied.seal != "" {
				t.Fatal("revoked target physical reader returned authority", err)
			}

			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			if err := lockProof(proof); err != nil {
				t.Fatal("restored target result authority", err)
			}
			if os.Getenv("PHOTO_TEST_V27_TARGET_PERSON_FINISH") == "1" {
				finish := func() error {
					locked, err := withNativeManualParentLock(ctx, p.Scheduler, d.request.TenantID, d.request.RunID, func(lockCtx context.Context) error {
						return database.InTransaction(lockCtx, p.Scheduler, func(tx pgx.Tx) error {
							return p.Reader.finishNativeTargetPersonInTransaction(lockCtx, tx, who, d, plan, transport.Graph.Built, proof)
						})
					})
					if err != nil {
						return err
					}
					if !locked {
						return fmt.Errorf("target finish parent lock unavailable")
					}
					return nil
				}
				var beforeUsage, residual int64
				var expiry time.Time
				if err := owner.QueryRow(ctx, "SELECT u.reserved_bytes,q.bytes,q.expires_at FROM photo.tenant_usage u JOIN photo.quota_reservations q ON q.tenant_id=u.tenant_id WHERE u.tenant_id=$1 AND q.holder_type='run' AND q.holder_id=$2", d.request.TenantID, plan.person.shell.Run).Scan(&beforeUsage, &residual, &expiry); err != nil {
					t.Fatal(err)
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", d.request.TenantID, plan.person.shell.Run); err != nil {
					t.Fatal(err)
				}
				if err := finish(); err == nil {
					t.Fatal("expired target reservation finalized")
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=$3 WHERE tenant_id=$1 AND holder_type='run' AND holder_id=$2", d.request.TenantID, plan.person.shell.Run, expiry); err != nil {
					t.Fatal(err)
				}

				if err := finish(); err != nil {
					t.Fatal("target finish", err)
				}
				if err := finish(); err != nil {
					t.Fatal("target finish replay", err)
				}
				var afterUsage int64
				if err := owner.QueryRow(ctx, "SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=$1", d.request.TenantID).Scan(&afterUsage); err != nil || afterUsage != beforeUsage-residual {
					t.Fatal("target residual quota release", beforeUsage, residual, afterUsage, err)
				}

				finalized, err := p.Reader.readNativeTargetPersonResult(ctx, who, plan)
				if err != nil || finalized.seal != proof.seal {
					t.Fatal("target finalized result reader", err)
				}
				if err := lockProof(finalized); err != nil {
					t.Fatal("target finalized result authority", err)
				}
				if err := invoke(true); err != nil {
					t.Fatal("target finalized seal replay", err)
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				if err := finish(); err == nil {
					t.Fatal("revoked target finish replay accepted")
				}
				if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
					t.Fatal(err)
				}
				if err := finish(); err != nil {
					t.Fatal("restored target finish replay", err)
				}
				t.Log("V27_TARGET_PERSON_PHYSICAL_FINALIZE_QUOTA_REPLAY_OK")
			}

			t.Log("V27_TARGET_PERSON_PHYSICAL_RESULT_AUTHORITY_OK")

			t.Log("V27_TARGET_PERSON_PHYSICAL_CANONICAL_EXECUTION_REPLAY_OK")
		}

	}

}
