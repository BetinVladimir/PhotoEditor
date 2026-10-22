package semanticreview

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
	"time"
)

func TestShownRemovalReviewReportReauthorizesAfterIOAndDeniesBeforeIO(t *testing.T) {
	sha := strings.Repeat("a", 64)
	issuer := uuid.New()
	body, _ := canonical.Marshal(v2qualification.Report{SchemaVersion: 1, Operation: "semantic.adopt_shown_removal", Version: "1", ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, InputFixtureSHAs: []string{strings.Repeat("e", 64)}, Checks: []v2qualification.Check{{ID: "quality", Passed: true, EvidenceSHA: strings.Repeat("f", 64)}}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)})
	digest, _ := canonical.Hash(json.RawMessage(body))
	pin := ExposureQualificationPin{PolicyID: uuid.New(), QualificationID: uuid.New(), IssuerID: issuer, PolicySHA: sha, Backend: "cpu", ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, Report: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest}, Calibration: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("6", 64)}}
	e := exposureReportEvidence{Pin: pin}
	e.Policy.SchemaVersion = 1
	e.Policy.PolicyID = pin.PolicyID
	e.Policy.Operation = "semantic.adopt_shown_removal"
	e.Policy.Version = "1"
	e.Policy.Backend = "cpu"
	e.Policy.Model = sha
	e.Policy.Runtime = sha
	e.Policy.Device = sha
	e.Policy.Dataset = sha
	e.Policy.Issuer = issuer
	e.Policy.Checks = []string{"quality"}
	e.Policy.Fixtures = []string{strings.Repeat("e", 64)}
	e.Policy.Calibrations = []string{pin.Calibration.SHA256}
	e.Policy.Evidence = []string{strings.Repeat("f", 64)}
	e.Policy.License = true
	e.Report.ID = pin.Report.ArtifactID
	e.Report.SHA = digest
	e.Report.Bucket = "photos"
	e.Report.Key = "report"
	e.Report.Version = "v1"
	e.Report.Bytes = int64(len(body))
	e.Report.Media = "application/json"
	metadata, _ := json.Marshal(e)
	tenant, intent, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	for _, scenario := range []string{"valid", "large_policy", "scope_changed", "tampered_report", "oversized", "wrong_operation", "cancel_during_read", "cancel_with_bad_bytes"} {
		t.Run(scenario, func(t *testing.T) {
			q := &exposureReportQuery{raw: metadata}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			if scenario == "large_policy" {
				changed := e
				changed.Policy.Fixtures = append([]string(nil), e.Policy.Fixtures...)
				for i := 0; i < 1000; i++ {
					changed.Policy.Fixtures = append(changed.Policy.Fixtures, fmt.Sprintf("%064x", i))
				}
				q.raw, _ = json.Marshal(changed)
				if len(q.raw) <= 65536 {
					t.Fatal("large policy fixture too small")
				}
			}
			if scenario == "wrong_operation" {
				changed := e
				changed.Policy.Operation = "semantic.adopt_shown_exposure"
				q.raw, _ = json.Marshal(changed)
			}
			if scenario == "oversized" {
				changed := e
				changed.Report.Bytes = 8<<20 + 1
				q.raw, _ = json.Marshal(changed)
			}
			read := func(_ context.Context, source PlanSource) ([]byte, error) {
				calls++
				if source.Version != "v1" || source.Ref != pin.Report {
					t.Fatal("report object pin changed")
				}
				if scenario == "scope_changed" {
					changed := e
					changed.Report.Version = "v2"
					q.raw, _ = json.Marshal(changed)
				}
				if scenario == "cancel_during_read" || scenario == "cancel_with_bad_bytes" {
					cancel()
					if scenario == "cancel_with_bad_bytes" { return []byte("invalid"), nil }
				}
				if scenario == "tampered_report" {
					return append(append([]byte(nil), body...), ' '), nil
				}
				return body, nil
			}
			err := VerifyShownRemovalReviewReport(ctx, q, read, "photos", tenant, intent, actor, session, 1, pin)
			switch scenario {
			case "valid", "large_policy":
				if err != nil || calls != 1 || q.calls != 2 {
					t.Fatal("valid report not rechecked", err)
				}
			case "cancel_during_read", "cancel_with_bad_bytes":
				if !errors.Is(err, context.Canceled) || calls != 1 || q.calls != 1 {
					t.Fatal("cancelled report proceeded to verification/requery", err, calls, q.calls)
				}
			case "scope_changed":
				if !errors.Is(err, ErrConflict) {
					t.Fatal("post-IO change accepted", err)
				}
			case "tampered_report":
				if !errors.Is(err, v2qualification.ErrUnqualified) {
					t.Fatal("tampered report accepted", err)
				}
			case "oversized", "wrong_operation":
				if !errors.Is(err, ErrInvalid) || calls != 0 {
					t.Fatal("oversized report reached IO", err)
				}
			}
		})
	}
}
