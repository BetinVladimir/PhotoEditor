package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestDonorQualificationGate(t *testing.T) {
	sha := strings.Repeat("a", 64)
	pin := DonorQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: uuid.New(), PolicySHA: sha,
		Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: sha},
		ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, Backend: "cpu"}
	who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
	tenant, project := uuid.New(), uuid.New()
	for _, mode := range []string{"ready", "stale", "role", "pin", "alias", "cancelled"} {
		p, ctx := pin, context.Background()
		tx := &donorIdentityTx{role: "photo_scheduler", ready: true}
		switch mode {
		case "stale":
			tx.ready = false
		case "role":
			tx.role = "photo_api"
		case "pin":
			p.RuntimeLockSHA = "invalid"
		case "alias":
			p.Report.ArtifactID = p.Calibration.ArtifactID
		case "cancelled":
			c, cancel := context.WithCancel(ctx)
			cancel()
			ctx = c
		}
		err := LockDonorQualification(ctx, tx, who, tenant, project, p)
		if mode == "ready" {
			if err != nil || tx.calls != 2 || len(tx.args) != 5 {
				t.Fatal("qualification gate", err)
			}
			var retained DonorQualificationPin
			if json.Unmarshal([]byte(tx.args[4].(string)), &retained) != nil || retained != pin {
				t.Fatal("pin changed before SQL")
			}
		} else if err == nil {
			t.Fatal("accepted invalid qualification", mode)
		}
		if (mode == "pin" || mode == "alias" || mode == "cancelled") && tx.calls != 0 {
			t.Fatal("invalid pin performed SQL", mode)
		}
		if mode == "role" && tx.calls != 1 {
			t.Fatal("API reached definer gate")
		}
	}
	for _, mode := range []string{"policy", "qualification", "issuer", "backend", "report_sha", "calibration_sha", "policy_sha", "model", "dataset", "device"} {
		p := pin
		switch mode {
		case "policy":
			p.PolicyID = uuid.Nil
		case "qualification":
			p.QualificationID = uuid.Nil
		case "issuer":
			p.IssuerID = uuid.Nil
		case "backend":
			p.Backend = "cuda"
		case "report_sha":
			p.Report.SHA256 = "invalid"
		case "calibration_sha":
			p.Calibration.SHA256 = "invalid"
		case "policy_sha":
			p.PolicySHA = "invalid"
		case "model":
			p.ModelLockSHA = "invalid"
		case "dataset":
			p.DatasetSHA = "invalid"
		case "device":
			p.DeviceProfileSHA = "invalid"
		}
		if body, err := p.Compile(); err == nil || body != nil {
			t.Fatal("compiled invalid pin", mode)
		}
	}
}
