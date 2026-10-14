package v2portrait

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

func TestRestorationQualificationRequiresDedicatedSchedulerAuthority(t *testing.T) {
	for _, operation := range []string{"portrait.glare", "image.reflection"} {
		pin := restorationPin(operation)
		who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
		tenant, project := uuid.New(), uuid.New()
		for _, mode := range []string{"ready", "stale", "role", "invalid", "cancelled"} {
			tx := &relightQualificationTx{donorIdentityTx: donorIdentityTx{
				role: "photo_scheduler", ready: true}}
			ctx := context.Background()
			current := pin
			switch mode {
			case "stale":
				tx.ready = false
			case "role":
				tx.role = "photo_api"
			case "invalid":
				current.PolicyID = uuid.Nil
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			err := LockRestorationQualification(ctx, tx, who, tenant, project, current)
			if mode == "ready" {
				if err != nil || tx.calls != 2 || len(tx.queries) != 2 ||
					tx.queries[1] != "SELECT photo.lock_v28_restoration_qualification($1,$2,$3,$4,$5,$6::jsonb)" ||
					len(tx.args) != 6 ||
					tx.args[0] != tenant || tx.args[1] != project ||
					tx.args[2] != who.UserID || tx.args[3] != who.SessionID ||
					tx.args[4] != operation {
					t.Fatal("scheduler authority binding changed", operation, err)
				}
				var bound RestorationQualificationPin
				if json.Unmarshal([]byte(tx.args[5].(string)), &bound) != nil || bound != pin {
					t.Fatal("qualification pin changed")
				}
			} else if err == nil {
				t.Fatal("invalid authority accepted", mode, operation)
			}
			if (mode == "invalid" || mode == "cancelled") && tx.calls != 0 {
				t.Fatal("invalid pin or cancellation reached SQL")
			}
			if mode == "role" && tx.calls != 1 {
				t.Fatal("API role reached definer")
			}
		}
	}
}

func restorationPin(operation string) RestorationQualificationPin {
	sha := strings.Repeat("a", 64)
	return RestorationQualificationPin{
		Operation: operation, OperationVersion: "1", PolicyID: uuid.New(),
		QualificationID: uuid.New(), PolicySHA: sha,
		Report:       v2input.Ref{ArtifactID: uuid.New(), SHA256: sha},
		Calibration:  v2input.Ref{ArtifactID: uuid.New(), SHA256: sha},
		ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha,
		DatasetSHA: sha, IssuerID: uuid.New(), Backend: "cpu",
	}
}

func restorationProfileFixture(t *testing.T, pin RestorationQualificationPin) ([]byte, []byte) {
	t.Helper()
	params, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": pin.ModelLockSHA,
		"calibration": pin.Calibration,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, err := canonical.Marshal(map[string]any{
		"backend": pin.Backend, "renderer_version": "v2.2",
		"resource_estimate": LocalResourceEstimate{
			PixelCount: 100, RAMBytes: 3 * 1024 * 1024 * 1024,
			ScratchBytes: 1024, OutputBytes: 1024,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return params, profile
}

func TestRestorationProfileBindsIndependentPin(t *testing.T) {
	for _, operation := range []string{"portrait.glare", "image.reflection"} {
		pin := restorationPin(operation)
		params, profile := restorationProfileFixture(t, pin)
		compiled, err := CompilePinnedRestorationProfile(operation, params, profile, pin)
		if err != nil {
			t.Fatal(operation, err)
		}
		var result struct {
			Pin        RestorationQualificationPin `json:"restoration_qualification"`
			RuntimeSHA string                      `json:"restoration_runtime_sha"`
		}
		if json.Unmarshal(compiled, &result) != nil || result.Pin != pin ||
			result.RuntimeSHA != pin.RuntimeLockSHA {
			t.Fatal("pin binding changed", operation)
		}
	}
}

func TestRestorationProfileRejectsCrossOperationAndChangedMaterial(t *testing.T) {
	pin := restorationPin("image.reflection")
	params, profile := restorationProfileFixture(t, pin)
	wrong := pin
	wrong.Operation = "portrait.open_eyes"
	wrongParams := pin
	wrongParams.ModelLockSHA = strings.Repeat("b", 64)
	wrongCalibration := pin
	wrongCalibration.Calibration.ArtifactID = uuid.New()
	for _, item := range []struct {
		op              string
		params, profile []byte
		pin             RestorationQualificationPin
	}{
		{"portrait.glare", params, profile, pin},
		{"image.reflection", params, profile, wrong},
		{"image.reflection", params, profile, wrongParams},
		{"image.reflection", params, profile, wrongCalibration},
		{"image.reflection", append([]byte(" "), params...), profile, pin},
		{"image.reflection", params, append([]byte(" "), profile...), pin},
	} {
		if _, err := CompilePinnedRestorationProfile(item.op, item.params, item.profile, item.pin); err == nil {
			t.Fatal("invalid pin/profile accepted", item.op)
		}
	}
}

func TestRestorationProfileRejectsUnadmittedRAMAndAlreadyPinnedProfile(t *testing.T) {
	pin := restorationPin("image.reflection")
	params, profile := restorationProfileFixture(t, pin)
	var fields map[string]json.RawMessage
	if json.Unmarshal(profile, &fields) != nil {
		t.Fatal("profile JSON")
	}
	fields["resource_estimate"], _ = canonical.Marshal(LocalResourceEstimate{
		PixelCount: 100, RAMBytes: 1, ScratchBytes: 1024, OutputBytes: 1024})
	tooSmall, _ := canonical.Marshal(fields)
	if _, err := CompilePinnedRestorationProfile(pin.Operation, params, tooSmall, pin); err == nil {
		t.Fatal("insufficient RAM accepted")
	}
	fields["resource_estimate"], _ = canonical.Marshal(LocalResourceEstimate{
		PixelCount: 100, RAMBytes: 3 * 1024 * 1024 * 1024,
		ScratchBytes: 1024, OutputBytes: 1024})
	fields["restoration_qualification"] = json.RawMessage(`{}`)
	alreadyPinned, _ := canonical.Marshal(fields)
	if _, err := CompilePinnedRestorationProfile(pin.Operation, params, alreadyPinned, pin); err == nil {
		t.Fatal("caller-supplied pin accepted")
	}
}
