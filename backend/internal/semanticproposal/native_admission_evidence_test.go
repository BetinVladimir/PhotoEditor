package semanticproposal

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func assertNativeAdmissionEvidence(t *testing.T, reader SQLTransferSourceReader, who auth.Identity, request Request, shell v2input.DraftShell, source sourcePreparationProof, operation string, marshal func(context.Context) (json.RawMessage, error)) {
	t.Helper()
	body, err := marshal(context.Background())
	if err != nil {
		t.Fatal("native admission packet", err)
	}
	var packet nativeAdmissionEvidence
	if err := json.Unmarshal(body, &packet); err != nil {
		t.Fatal(err)
	}
	pins, err := marshalSourceEvidencePins(source)
	if err != nil {
		t.Fatal(err)
	}
	if packet.SchemaVersion != 1 || packet.Operation != operation || packet.Tenant != request.TenantID || packet.Project != reader.ProjectID || packet.Actor != who.UserID || packet.Session != who.SessionID || packet.Run != shell.Run || packet.ParentRun != request.RunID || packet.RequestSHA != request.RequestSHA || packet.ConfigText != string(shell.Config) || packet.Anchor.IntentText != string(request.Body) || packet.Binding != source.binding || packet.ICCSHA != source.iccSHA || !packet.UnitRGB || string(packet.SourcePins) != string(pins) {
		t.Fatal("native authority packet changed original scope")
	}
	policySHA, err := canonical.Hash(json.RawMessage(packet.PolicyText))
	if err != nil || policySHA != shell.PolicySHA {
		t.Fatal("native packet policy mismatch", err)
	}
	var registration defectRegistrationBinding
	if json.Unmarshal(packet.Registration, &registration) != nil || registration.TaskID != shell.Tasks[0].ID || registration.InputDigest != shell.Tasks[0].InputDigest || string(registration.Profile) != string(shell.Tasks[0].ExecutionProfile) {
		t.Fatal("native packet lost registered task")
	}
	var files []v2draft.InputEvidence
	if json.Unmarshal(packet.Files, &files) != nil || len(files) < 3 {
		t.Fatal("native packet lost file closure")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := marshal(ctx); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled packet", err)
	}
}

func mutateNativeSourceScope(kind string, source *sourcePreparationProof) {
	switch kind {
	case "source_tenant":
		source.tenant = uuid.New()
	case "source_project":
		source.project = uuid.New()
	case "source_actor":
		source.actor = uuid.New()
	case "source_session":
		source.session = uuid.New()
	case "source_bucket":
		source.bucket = "foreign-bucket"
	case "icc":
		source.iccSHA = "invalid"
	case "unit_rgb":
		source.unitRGB = false
	default:
		panic("unknown source mutation")
	}
}
