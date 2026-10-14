package semanticproposal

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestSavedDetectorAnchorLocatorsRefuseChangedOrMissingIdentity(t *testing.T) {
	profile := json.RawMessage(`{"backend":"cuda","resource_estimate":{"ram_bytes":1024,"vram_bytes":0,"scratch_bytes":2048,"output_bytes":4096}}`)
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	report, intent, snapshot := pin(), pin(), pin()
	metadata, err := nativePlanRecoveryFromTask(v2input.DraftTask{Operation: "face.detect", ExecutionProfile: profile}, &report, v2input.SemanticQualifiedCosts{}, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	metadata.RequestKey = "original-request"
	encode := func(m nativePlanRecoveryMetadata, i, s v2input.Ref) nativeInputDetectorRecoveryRow {
		body, err := canonical.Marshal(map[string]any{"v27_detector_prepare": map[string]any{"intent": i, "snapshot": s, "recovery": m}})
		if err != nil {
			t.Fatal(err)
		}
		sha, err := canonical.Hash(json.RawMessage(body))
		if err != nil {
			t.Fatal(err)
		}
		return nativeInputDetectorRecoveryRow{config: body, sha: sha, profile: profile}
	}
	original := encode(metadata, intent, snapshot)
	key, i, s, err := nativeInputDetectorAnchorLocators(original)
	if err != nil || key != metadata.RequestKey || i != intent || s != snapshot {
		t.Fatal("original locators refused", err)
	}
	legacy := metadata
	legacy.RequestKey = ""
	invalidRef := intent
	invalidRef.SHA256 = "invalid"
	changedSHA := original
	changedSHA.sha = strings.Repeat("f", 64)
	unknown := original
	unknown.profile = json.RawMessage(`{}`)
	for _, row := range []nativeInputDetectorRecoveryRow{encode(legacy, intent, snapshot), encode(metadata, v2input.Ref{}, snapshot), encode(metadata, intent, v2input.Ref{}), encode(metadata, invalidRef, snapshot), changedSHA, unknown, {}} {
		key, i, s, err := nativeInputDetectorAnchorLocators(row)
		if err == nil || key != "" || i != (v2input.Ref{}) || s != (v2input.Ref{}) {
			t.Fatal("invalid locators exposed recovery inputs", err)
		}
	}
}

func TestSavedAnchorRecoveryCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := SQLTransferSourceReader{}
	if row, err := reader.readNativeInputDetectorRecoveryRow(ctx, auth.Identity{}, uuid.New(), uuid.New()); err != context.Canceled || row.config != nil {
		t.Fatal("cancelled locator reached IO", err)
	}
	if anchor, err := reader.loadNativePreparationAnchor(ctx, auth.Identity{}, uuid.New(), uuid.New(), TransferColorProfile{}, 1024); err != context.Canceled || anchor.Key != "" || anchor.SnapshotBody != nil {
		t.Fatal("cancelled anchor exposed state", err)
	}
	if pin, err := reader.nativeInputSavedAnchorObject(ctx, auth.Identity{}, uuid.New(), uuid.New(), "", "intent", v2input.Ref{}); err != context.Canceled || pin.ref.ArtifactID != uuid.Nil {
		t.Fatal("cancelled document locator reached IO", err)
	}
	if out, err := (TransferDraftPreparer{}).advanceSavedNativeManualParent(ctx, nil, auth.Identity{}, uuid.New(), uuid.New()); err != context.Canceled || out.ticket != (v2input.JobTicket{}) {
		t.Fatal("cancelled restart exposed ticket", err)
	}
}

func TestPersonAnchorLocatorsRequireOriginalBoundedMetadata(t *testing.T) {
	profile := json.RawMessage(`{"backend":"cuda","resource_estimate":{"ram_bytes":1024,"vram_bytes":256,"scratch_bytes":2048,"output_bytes":4096}}`)
	pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
	report, intent, snapshot := pin(), pin(), pin()
	metadata, err := nativePlanRecoveryFromTask(v2input.DraftTask{Operation: "mask.person", ExecutionProfile: profile}, &report, v2input.SemanticQualifiedCosts{VRAMBytes: 256}, 16<<20)
	if err != nil {
		t.Fatal(err)
	}
	metadata.RequestKey = "person-original-request"
	encode := func(m nativePlanRecoveryMetadata, i, s v2input.Ref) nativeInputDetectorRecoveryRow {
		body, err := canonical.Marshal(map[string]any{"v27_person_prepare": map[string]any{"intent": i, "snapshot": s, "recovery": m}})
		if err != nil {
			t.Fatal(err)
		}
		sha, err := canonical.Hash(json.RawMessage(body))
		if err != nil {
			t.Fatal(err)
		}
		return nativeInputDetectorRecoveryRow{config: body, sha: sha, profile: profile}
	}
	original := encode(metadata, intent, snapshot)
	key, i, s, err := nativePreparationAnchorLocators(original, "mask.person")
	if err != nil || key != metadata.RequestKey || i != intent || s != snapshot {
		t.Fatal("person original locators lost", err)
	}
	legacy := metadata
	legacy.RequestKey = ""
	invalid := intent
	invalid.SHA256 = "invalid"
	changed := original
	changed.sha = strings.Repeat("f", 64)
	for _, row := range []nativeInputDetectorRecoveryRow{encode(legacy, intent, snapshot), encode(metadata, v2input.Ref{}, snapshot), encode(metadata, intent, v2input.Ref{}), encode(metadata, invalid, snapshot), changed, {}} {
		key, i, s, err := nativePreparationAnchorLocators(row, "mask.person")
		if err == nil || key != "" || i != (v2input.Ref{}) || s != (v2input.Ref{}) {
			t.Fatal("invalid person locator exposed request", err)
		}
	}
	for _, op := range []string{"face.detect", "face.parse", "unknown"} {
		if key, _, _, err := nativePreparationAnchorLocators(original, op); err == nil || key != "" {
			t.Fatal("wrong stage accepted person locators", op)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if anchor, err := (SQLTransferSourceReader{}).loadPersonPreparationAnchor(ctx, auth.Identity{}, uuid.New(), uuid.New(), TransferColorProfile{}, 1024); err != context.Canceled || anchor.Request.RunID != uuid.Nil {
		t.Fatal("cancelled person anchor reached IO", err)
	}
}
