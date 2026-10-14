package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestReadReadySigningPageRequiresCurrentLeaseAndRequiredPolicy(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := (PreparePlan{}).ReadReadySigningPage(ctx, nil,
		ExpansionLease{}, 0, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled page accepted", err)
	}
	if _, _, err := (PreparePlan{}).ReadReadySigningPage(
		context.Background(), nil, ExpansionLease{}, 0, 1); err == nil {
		t.Fatal("unscoped page accepted")
	}
}

func TestReadReadySigningPageRequiresStableCurrentExport(t *testing.T) {
	plan, lease, payload := expansionPlanFixture(t)
	lease.Cursor = lease.SlotCount
	var frozen FrozenInput
	if err := json.Unmarshal([]byte(payload.Proof.FrozenInputJCS), &frozen); err != nil {
		t.Fatal(err)
	}
	frozen.Policy.Provenance = "required"
	frozenRaw, err := canonical.Marshal(frozen)
	if err != nil { t.Fatal(err) }
	payload.Proof.FrozenInputJCS = string(frozenRaw)
	plan.proofJCS, err = canonical.Marshal(payload.Proof)
	if err != nil { t.Fatal(err) }
	page, err := plan.ExportChildren(lease.Run, 0, 1)
	if err != nil || len(page.Items) != 1 { t.Fatal(err) }
	child := page.Items[0]
	exportID, err := plan.NewChildExportID(lease.Run, child)
	if err != nil { t.Fatal(err) }
	artifact := v2input.Ref{ArtifactID: uuid.New(), SHA256: digest([]byte("encoded"))}
	base := ExportObservation{ExportID: exportID,
		SnapshotDigest: child.SnapshotDigest, Binding: child.Binding,
		PresetSHA: child.Preset.PresetSHA, RunState: "completed",
		ExportState: "ready", ArtifactReady: true, Artifact: &artifact}
	for _, mode := range []string{"ready", "pending", "changed", "lost"} {
		t.Run(mode, func(t *testing.T) {
			store := &matrixObservationStore{observation: base,
				pending: mode == "pending", change: mode == "changed",
				fail: mode == "lost"}
			slots, next, err := plan.ReadReadySigningPage(context.Background(),
				store, lease, 0, 1)
			if mode == "changed" || mode == "lost" {
				if err == nil || len(slots) != 0 { t.Fatal(slots, err) }
				return
			}
			if err != nil || next != 1 || store.calls != 2 { t.Fatal(slots, next, err) }
			if mode == "ready" && (len(slots) != 1 || slots[0].ExportID == nil ||
				*slots[0].ExportID != exportID) { t.Fatal(slots) }
			if mode == "pending" && len(slots) != 0 { t.Fatal(slots) }
		})
	}
}
