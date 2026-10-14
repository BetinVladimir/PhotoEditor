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
	"reflect"
	"testing"
)

func probeNativeTargetDefectTransport(t *testing.T, ctx context.Context, p TransferDraftPreparer, who auth.Identity, original detectorDraftPlan, plan nativeTargetDefectDraftPlan, dependencies nativeTargetDefectDependencies, owner *pgxpool.Pool) {
	t.Helper()
	publish := func() (v2input.PreparedSemanticTask, defectRegistrationProof, error) {
		return p.Reader.publishOwnedNativeTargetDefectTransport(ctx, who, original, plan, p.Configuration.Color, v2input.ScopedPublisher{Documents: p.TransportDocuments, Tenant: plan.shell.Tenant, Run: plan.shell.Run})
	}
	published, proof, err := publish()
	if err != nil {
		t.Fatal("target defect physical transport", err)
	}
	again, replayed, err := publish()
	if err != nil || !reflect.DeepEqual(replayed, proof) || !reflect.DeepEqual(again.Graph.Built, published.Graph.Built) {
		t.Fatal("target defect transport replay", err)
	}
	binding, err := marshalNativeTargetDefectRegistrationBinding(ctx, plan, published.Graph.Built)
	if err != nil {
		t.Fatal("target defect transport binding", err)
	}
	repeated, err := marshalNativeTargetDefectRegistrationBinding(ctx, plan, again.Graph.Built)
	if err != nil || string(repeated) != string(binding) {
		t.Fatal("target defect binding replay", err)
	}
	for _, kind := range []string{"manifest", "count", "digest", "chunk"} {
		changed := published.Graph.Built
		switch kind {
		case "manifest":
			changed.Manifest.SHA256 = "invalid"
		case "count":
			changed.Count++
		case "digest":
			changed.SHA256 = "changed"
		case "chunk":
			changed.Chunks = append([]v2input.Ref(nil), changed.Chunks...)
			changed.Chunks[0].SHA256 = "invalid"
		}
		if _, err := marshalNativeTargetDefectRegistrationBinding(ctx, plan, changed); err == nil {
			t.Fatal("target defect substituted transport accepted", kind)
		}
	}
	// Valid-looking foreign locators are serialization, not authority. They
	// produce a different binding and require exact backend-owner SQL checks.
	foreign := published.Graph.Built
	foreign.Manifest.ArtifactID = uuid.New()
	foreignBinding, err := marshalNativeTargetDefectRegistrationBinding(ctx, plan, foreign)
	if err != nil || string(foreignBinding) == string(binding) {
		t.Fatal("target defect foreign locator binding", err)
	}
	packet, err := p.Reader.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, dependencies, published.Graph.Built)
	if err != nil {
		t.Fatal("target defect admission packet", err)
	}
	packetAgain, err := p.Reader.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, dependencies, again.Graph.Built)
	if err != nil || string(packetAgain) != string(packet) {
		t.Fatal("target defect admission packet replay", err)
	}
	for _, kind := range []string{"person", "landmarks", "skin", "preparation", "transport"} {
		changed := dependencies
		switch kind {
		case "person":
			changed.person = "{}"
		case "landmarks":
			changed.landmarks = "{}"
		case "skin":
			changed.skin = "{}"
		case "preparation":
			changed.preparation.seal = "changed"
		case "transport":
			changed.transports.person.Count++
		}
		if _, err := p.Reader.marshalNativeTargetDefectAdmissionEvidence(ctx, who, original, plan, changed, published.Graph.Built); err == nil {
			t.Fatal("target defect admission substituted dependency accepted", kind)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := p.Reader.marshalNativeTargetDefectAdmissionEvidence(cancelled, who, original, plan, dependencies, published.Graph.Built); err == nil {
		t.Fatal("target defect cancelled admission packet accepted")
	}
	t.Log("V27_TARGET_DEFECT_ADMISSION_PACKET_REPLAY_SUBSTITUTION_OK")
	var inputs, queued int
	if err := owner.QueryRow(ctx, "SELECT (SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2),(SELECT count(*) FROM photo.outbox WHERE tenant_id=$1 AND payload->>'run_id'=$3::text AND routing_key='v2.run.queued')", plan.shell.Tenant, plan.shell.Tasks[0].ID, plan.shell.Run).Scan(&inputs, &queued); err != nil || inputs != 0 || queued != 0 {
		t.Fatal("target defect premature registration/queue", inputs, queued, err)
	}
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	_, _, revokedErr := publish()
	if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
		t.Fatal(err)
	}
	if revokedErr == nil {
		t.Fatal("target defect revoked transport publication accepted")
	}
	if _, _, err := publish(); err != nil {
		t.Fatal("target defect restored transport publication", err)
	}

	if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_REGISTER") == "1" {
		register := func() error {
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					return p.Reader.registerNativeTargetDefectInputsInTransaction(work, tx, who, original, plan, dependencies, p.Configuration.Color, published.Graph.Built)
				})
			})
			if err == nil && !ok {
				t.Fatal("target defect register parent lock missing")
			}
			return err
		}
		state := func(body []byte) string {
			var result string
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					if _, err := p.Reader.beginNativeTargetDefectDraftInTransaction(work, tx, who, original, plan, dependencies, p.Configuration.Color); err != nil {
						return err
					}
					return tx.QueryRow(work, "SELECT photo.semantic_target_defect_registration_binding_state($1,$2,'defects.propose',$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(body)).Scan(&result)
				})
			})
			if err != nil || !ok {
				t.Fatal("target defect SQL binding state", err)
			}
			return result
		}
		if state(binding) != "missing" || state(foreignBinding) != "conflict" {
			t.Fatal("target defect SQL transport ownership gate")
		}
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		revokedErr := register()
		if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
			t.Fatal(err)
		}
		if revokedErr == nil {
			t.Fatal("target defect revoked registration accepted")
		}
		if _, err := owner.Exec(ctx, "INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES($1,$2,$3,$4,$5,'Params',0)", plan.shell.Tenant, plan.shell.Project, plan.shell.Tasks[0].ID, plan.prepared.params.ref.ArtifactID, plan.prepared.params.ref.SHA256); err != nil {
			t.Fatal("target defect partial fixture", err)
		}
		partialErr := register()
		var partialCount int
		if err := owner.QueryRow(ctx, "SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", plan.shell.Tenant, plan.shell.Tasks[0].ID).Scan(&partialCount); err != nil {
			t.Fatal(err)
		}
		if _, err := owner.Exec(ctx, "DELETE FROM photo.v2_task_inputs WHERE tenant_id=$1 AND task_id=$2", plan.shell.Tenant, plan.shell.Tasks[0].ID); err != nil {
			t.Fatal("target defect restore partial fixture", err)
		}
		if partialErr == nil || partialCount != 1 {
			t.Fatal("target defect partial registration repaired", partialErr, partialCount)
		}
		if err := register(); err != nil {
			t.Fatal("target defect registration", err)
		}
		if err := register(); err != nil {
			t.Fatal("target defect registration replay", err)
		}
		if state(binding) != "replayed" || state(foreignBinding) != "conflict" {
			t.Fatal("target defect registered binding exactness")
		}
		var registered int
		var status, taskState string
		var sealed bool
		if err := owner.QueryRow(ctx, "SELECT r.status,r.graph_sealed,t.state,(SELECT count(*) FROM photo.v2_task_inputs i WHERE i.tenant_id=t.tenant_id AND i.task_id=t.id AND i.ordinal<1000000) FROM photo.runs r JOIN photo.tasks t ON t.tenant_id=r.tenant_id AND t.run_id=r.id WHERE r.tenant_id=$1 AND r.id=$2", plan.shell.Tenant, plan.shell.Run).Scan(&status, &sealed, &taskState, &registered); err != nil || status != "draft" || sealed || taskState != "blocked" || registered != published.Graph.Built.Count {
			t.Fatal("target defect registered blocked state", status, sealed, taskState, registered, err)
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_ADMISSION") == "1" {
			check := func(body []byte) bool {
				var ready bool
				ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
					return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
						if _, err := tx.Exec(work, "SELECT set_config('app.tenant_id',$1,true)", plan.shell.Tenant.String()); err != nil {
							return err
						}
						return tx.QueryRow(work, "SELECT photo.lock_semantic_target_defect_admission_evidence($1,$2,$3::jsonb)", plan.shell.Tenant, plan.shell.Run, string(body)).Scan(&ready)
					})
				})
				if err != nil || !ok {
					t.Fatal("target defect SQL admission", err)
				}
				return ready
			}
			if !check(packet) || !check(packet) {
				t.Fatal("target defect SQL admission refused exact replay")
			}
			for _, field := range []string{"person", "landmarks", "skin", "policy", "qualification", "qualification_pin", "params_pin", "registration", "config_text", "schema_version"} {
				var changed map[string]json.RawMessage
				if err := json.Unmarshal(packet, &changed); err != nil {
					t.Fatal(err)
				}
				changed[field] = json.RawMessage("null")
				body, err := json.Marshal(changed)
				if err != nil {
					t.Fatal(err)
				}
				if check(body) {
					t.Fatal("target defect SQL admission substituted packet accepted", field)
				}
			}
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			revokedReady := check(packet)
			if _, err := owner.Exec(ctx, "UPDATE photo.sessions SET revoked_at=NULL WHERE id=$1", who.SessionID); err != nil {
				t.Fatal(err)
			}
			if revokedReady || !check(packet) {
				t.Fatal("target defect SQL admission session fence")
			}
			if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=$1 AND holder_id=$2 AND holder_type='run'", plan.shell.Tenant, plan.shell.Run); err != nil {
				t.Fatal(err)
			}
			expiredReady := check(packet)
			if _, err := owner.Exec(ctx, "UPDATE photo.quota_reservations SET expires_at=clock_timestamp()+interval '1 hour' WHERE tenant_id=$1 AND holder_id=$2 AND holder_type='run'", plan.shell.Tenant, plan.shell.Run); err != nil {
				t.Fatal(err)
			}
			if expiredReady || !check(packet) {
				t.Fatal("target defect SQL admission quota fence")
			}
			ok, err := withNativeManualParentLock(ctx, p.Scheduler, original.request.TenantID, original.request.RunID, func(work context.Context) error {
				return database.InTransaction(work, p.Scheduler, func(tx pgx.Tx) error {
					return p.Reader.lockNativeTargetDefectAdmissionInTransaction(work, tx, who, original, plan, dependencies, published.Graph.Built)
				})
			})
			if err != nil || !ok {
				t.Fatal("target defect Go admission adapter", err)
			}
			t.Log("V27_TARGET_DEFECT_ADMISSION_REPLAY_SUBSTITUTION_REVOKE_QUOTA_OK")
		}
		if os.Getenv("PHOTO_TEST_V27_TARGET_DEFECT_SEAL") == "1" {
			probeNativeTargetDefectSeal(t, ctx, p, who, original, plan, dependencies, published.Graph.Built, owner)
		}
		t.Log("V27_TARGET_DEFECT_REGISTRATION_OWNERSHIP_REPLAY_REVOKE_OK")
	}
	t.Log("V27_TARGET_DEFECT_PHYSICAL_TRANSPORT_BINDING_REPLAY_REVOKE_OK")
}
