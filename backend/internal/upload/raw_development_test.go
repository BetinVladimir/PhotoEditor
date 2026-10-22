package upload

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/rawdevelopment"
	"testing"
)

func TestRAWUploadNormalizationAndCompletionSnapshot(t *testing.T) {
	policy := rawdevelopment.Policy{SchemaVersion: 1, Demosaic: "ahd", WBSource: "camera", ExposureEV: 1.5, HighlightPolicy: "blend", DecoderVersion: "rawpy-0.27.1/libraw-0.21.4"}
	request := BeginRequest{ProjectID: uuid.New(), Filename: "frame.nef", Bytes: 1024, Format: "nef", RAWDevelopment: &policy}
	normalized, _, err := normalize(request)
	if err != nil {
		t.Fatal(err)
	}
	policy.ExposureEV = 2
	if normalized.RAWDevelopment.ExposureEV != 1.5 {
		t.Fatal("normalization retained mutable caller policy")
	}
	lease := CompletionLease{ExpectedBytes: 1024, Filename: "frame.nef", MediaType: formats["nef"], ArtifactID: uuid.New(), RAWDevelopment: normalized.RAWDevelopment}
	raw, digest, _, _, err := completionDocuments(lease, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil || digest == "" {
		t.Fatal(err)
	}
	var payload struct {
		Version int                    `json:"schema_version"`
		Policy  *rawdevelopment.Policy `json:"raw_development"`
	}
	if err = json.Unmarshal(raw, &payload); err != nil || payload.Version != 2 || payload.Policy == nil || *payload.Policy != *normalized.RAWDevelopment {
		t.Fatal("RAW snapshot lost", err)
	}
	request.Filename = "frame.jpg"
	request.Format = "jpg"
	if _, _, err = normalize(request); err == nil {
		t.Fatal("RAW policy accepted for raster")
	}
	lease.RAWDevelopment = nil
	raw, _, _, _, err = completionDocuments(lease, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if err != nil {
		t.Fatal(err)
	}
	var legacy map[string]json.RawMessage
	if err = json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if string(legacy["schema_version"]) != "1" || legacy["raw_development"] != nil {
		t.Fatal("legacy snapshot changed")
	}
}

type substitutedRAWAdmission struct {
	mode  string
	calls int
}

func (a *substitutedRAWAdmission) Admit(ctx context.Context, tx rawdevelopment.ReportRows, tenantID, projectID, actor, session uuid.UUID, p *rawdevelopment.Policy) (*rawdevelopment.Policy, error) {
	a.calls++
	if a.mode == "nil" {
		return nil, nil
	}
	if a.mode == "changed" {
		p.ExposureEV = 3
	}
	return p, nil
}
func TestOwnerRAWAdmissionRequiresUnchangedQualifiedPolicy(t *testing.T) {
	for _, mode := range []string{"missing", "nil", "changed", "valid", "legacy", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			policy := &rawdevelopment.Policy{1, "ahd", "camera", 1.5, "blend", "rawpy-0.27.1/libraw-0.21.4"}
			adapter := &substitutedRAWAdmission{mode: mode}
			service := Service{RAWAdmission: adapter}
			if mode == "missing" {
				service.RAWAdmission = nil
			}
			if mode == "legacy" {
				policy = nil
			}
			if mode == "cancelled" {
				cancel()
			}
			out, err := service.admitRAW(ctx, nil, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(), policy)
			if mode == "valid" {
				if err != nil || out == nil || out == policy || *out != *policy {
					t.Fatal("qualified policy changed", err)
				}
			} else if mode == "legacy" {
				if err != nil || out != nil {
					t.Fatal("legacy changed")
				}
			} else if err == nil || out != nil {
				t.Fatal("invalid admission accepted")
			}
			if policy != nil && policy.ExposureEV != 1.5 {
				t.Fatal("caller mutated")
			}
			if (mode == "legacy" || mode == "cancelled") && adapter.calls != 0 {
				t.Fatal("unexpected admission I/O")
			}
		})
	}
}
