package v2portrait

import (
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestRelightPreparationRequestForbidsCalibrationAndAuthority(t *testing.T) {
	params, _, _, _, _ := relightQualificationFixture(t)
	request := RelightPreparationRequest{SchemaVersion: 1, Snapshot: uuid.New(), Base: params.Base, Depth: params.Depth, Subject: params.Subject, Protected: params.Protected, SubjectKind: "manual"}
	raw, _ := canonical.Marshal(request)
	if _, err := DecodeRelightPreparationRequest(raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"calibration", "qualification", "run_id", "tenant_id", "capacity"} {
		var fields map[string]any
		json.Unmarshal(raw, &fields)
		fields[key] = "injected"
		bad, _ := canonical.Marshal(fields)
		if _, err := DecodeRelightPreparationRequest(bad); err == nil {
			t.Fatal("accepted", key)
		}
	}
	request.SubjectKind = "invalid"
	bad, _ := canonical.Marshal(request)
	if _, err := DecodeRelightPreparationRequest(bad); err == nil {
		t.Fatal("invalid subject")
	}
}
