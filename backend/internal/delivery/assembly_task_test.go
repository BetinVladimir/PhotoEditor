package delivery

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestBlockedAssemblyTaskPinsIdentityParamsAndOutputReservation(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	params := v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("root"))}}
	body, err := canonical.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil {
		t.Fatal(err)
	}
	task := v2input.DraftTask{ID: owner, NodeKey: "v2.delivery.assemble:0", Operation: "v2.delivery.assemble", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: uuid.New(), ParamsSHA: digest(body), InputDigest: digest([]byte("inventory")), ExecutionProfile: []byte(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"vram_bytes":0,"output_bytes":8388608}}`)}
	for _, scenario := range []string{"accepted", "stale", "identity", "cursor", "params", "profile", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			db := &assemblyRetainDB{accepted: scenario != "stale"}
			current := lease
			candidate := task
			raw := append([]byte(nil), body...)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "identity":
				candidate.ID = uuid.New()
			case "cursor":
				current.Cursor = 0
			case "params":
				raw = append(raw, ' ')
			case "profile":
				candidate.ExecutionProfile = []byte(`{"backend":"mps"}`)
			case "cancel":
				cancel()
			}
			receiptDB := &assemblyRetainDB{accepted: scenario != "stale"}
			acknowledged, receiptErr := (ExpansionLeaseStore{DB: receiptDB}).AssemblyActivated(ctx, current, plan, candidate, raw)
			if scenario == "accepted" || scenario == "stale" {
				if receiptErr != nil || acknowledged != (scenario == "accepted") || receiptDB.calls != 1 {
					t.Fatal("wrong activation receipt", receiptErr)
				}
			} else if receiptErr == nil || receiptDB.calls != 0 {
				t.Fatal("invalid receipt reached SQL")
			}
			activationDB := &assemblyRetainDB{accepted: scenario != "stale"}
			activationErr := (ExpansionLeaseStore{DB: activationDB}).ActivateAssembly(ctx, current, plan, candidate, raw)
			if scenario == "accepted" {
				if activationErr != nil || activationDB.calls != 1 || len(activationDB.args) != 4 {
					t.Fatal("valid activation rejected", activationErr)
				}
			} else {
				if activationErr == nil {
					t.Fatal("invalid activation accepted")
				}
				if scenario != "stale" && activationDB.calls != 0 {
					t.Fatal("invalid activation reached SQL")
				}
			}
			ready, readErr := (ExpansionLeaseStore{DB: db}).AssemblyTransportReady(ctx, current, plan, candidate, raw)
			if scenario == "accepted" {
				if readErr != nil || !ready || len(db.args) != 4 {
					t.Fatal("ready transport rejected", readErr)
				}
			} else if scenario == "stale" {
				if readErr != nil || ready {
					t.Fatal("stale transport accepted", readErr)
				}
			} else if readErr == nil || db.calls != 0 {
				t.Fatal("invalid transport reached SQL")
			}
			db.calls = 0
			db.args = nil
			err := (ExpansionLeaseStore{DB: db}).CreateBlockedAssemblyTask(ctx, current, plan, candidate, raw)
			if scenario == "accepted" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("invalid task accepted", scenario)
			}
			if scenario == "accepted" || scenario == "stale" {
				if len(db.args) != 5 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[3] != string(body) || db.args[4] != int64(8388608) {
					t.Fatal("task scope drift", db.args)
				}
			} else if db.calls != 0 {
				t.Fatal("invalid task reached SQL")
			}
		})
	}
}

func TestSignedAssemblyTransportUsesDedicatedReceipt(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	params := v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("signed-envelope"))}}
	body, err := canonical.Marshal(params)
	if err != nil { t.Fatal(err) }
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil { t.Fatal(err) }
	task := v2input.DraftTask{ID: owner, NodeKey: "v2.delivery.assemble:0", Operation: "v2.delivery.assemble",
		OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: uuid.New(), ParamsSHA: digest(body),
		InputDigest: digest([]byte("signed-inventory")), ExecutionProfile: []byte(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"vram_bytes":0,"output_bytes":8388608}}`)}
	db := &assemblyRetainDB{accepted: true}
	ready, err := (ExpansionLeaseStore{DB: db}).SignedAssemblyTransportReady(context.Background(), lease, plan, task, body)
	if err != nil || !ready || db.calls != 1 || len(db.args) != 4 || db.args[0] != lease.Expansion ||
		db.args[1] != lease.Token || db.args[3] != string(body) { t.Fatal("signed transport scope", ready, err) }
	db.calls = 0
	task.ParamsSHA = digest([]byte("foreign"))
	if _, err := (ExpansionLeaseStore{DB: db}).SignedAssemblyTransportReady(context.Background(), lease, plan, task, body); err == nil || db.calls != 0 {
		t.Fatal("foreign signed Params reached SQL")
	}
}
