package delivery

import (
	"context"
	"github.com/google/uuid"
	"testing"
)

func TestFailPackageRechecksWitnessIdentityBeforeSQL(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	export, err := plan.NewChildExportID(lease.Run, page.Items[0])
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"accepted", "stale", "child", "export", "slot", "reason", "cancel"} {
		t.Run(scenario, func(t *testing.T) {
			db := &assemblyRetainDB{accepted: scenario != "stale"}
			witness := FailureWitness{SlotIndex: 0, ChildRun: page.Items[0].RunID, ExportID: export, Reason: "EXPORT_FAILED"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "child":
				witness.ChildRun = uuid.New()
			case "export":
				witness.ExportID = uuid.New()
			case "slot":
				witness.SlotIndex = lease.Cursor
			case "reason":
				witness.Reason = "untrusted reason"
			case "cancel":
				cancel()
			}
			err := (ExpansionLeaseStore{DB: db}).FailPackage(ctx, lease, plan, witness)
			if scenario == "accepted" {
				if err != nil || db.calls != 1 || len(db.args) != 3 {
					t.Fatal(err, db.calls)
				}
			} else {
				if err == nil {
					t.Fatal("invalid failure accepted")
				}
				if scenario != "stale" && db.calls != 0 {
					t.Fatal("invalid witness reached SQL")
				}
			}
		})
	}
}
