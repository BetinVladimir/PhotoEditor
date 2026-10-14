package delivery

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestDeliveryChildConfigAndFencedCursorCommit(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	child := page.Items[0]
	exportID := uuid.New()
	raw, sha, err := plan.ChildConfig(lease.Run, child, exportID)
	if err != nil || sha != digest(raw) {
		t.Fatal("invalid child config", err)
	}
	var config struct {
		QueueClass string `json:"queue_class"`
		ProjectID uuid.UUID           `json:"project_id"`
		Request   FrozenExportRequest `json:"v30_delivery_export"`
		ExportID  uuid.UUID           `json:"export_id"`
	}
	if json.Unmarshal(raw, &config) != nil || config.QueueClass != "batch" || config.ProjectID == uuid.Nil || config.ExportID != exportID || config.Request.ChildRunID != child.RunID {
		t.Fatal("scope lost")
	}
	db := &proofTx{accepted: true}
	store := ExpansionLeaseStore{DB: db}
	if err := store.CommitChild(context.Background(), lease, plan, child, exportID); err != nil {
		t.Fatal(err)
	}
	_, requestRaw, err := plan.ExportRequest(lease.Run, child)
	if err != nil {
		t.Fatal(err)
	}
	if len(db.args) != 5 || db.args[0] != lease.Expansion || db.args[1] != lease.Token || db.args[2] != child.SlotIndex || db.args[3] != string(requestRaw) || db.args[4] != exportID {
		t.Fatal("cursor fence/request changed")
	}
	db.accepted = false
	if err := store.CommitChild(context.Background(), lease, plan, child, exportID); err == nil {
		t.Fatal("stale SQL result accepted")
	}
}

func TestDeliveryChildCommitRejectsForeignIntentBeforeSQL(t *testing.T) {
	plan, lease, _ := expansionPlanFixture(t)
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*ExportChildIntent){
		func(c *ExportChildIntent) { c.RunID = uuid.New() },
		func(c *ExportChildIntent) { c.SlotIndex = 1 },
		func(c *ExportChildIntent) { c.Preset.PresetSHA = digest([]byte("foreign")) },
		func(c *ExportChildIntent) { c.Binding.AssetVersionID = uuid.New() },
	} {
		child := page.Items[0]
		change(&child)
		db := &proofTx{accepted: true}
		if err := (ExpansionLeaseStore{DB: db}).CommitChild(context.Background(), lease, plan, child, uuid.New()); err == nil || db.calls != 0 {
			t.Fatal("foreign child reached SQL")
		}
	}
	if _, _, err := plan.ChildConfig(lease.Run, page.Items[0], uuid.Nil); err == nil {
		t.Fatal("missing export accepted")
	}
}
