package provenance

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

func TestSigningRenderReportRequiresExactSourceAndParentEvidence(t *testing.T) {
	for _, mode := range []string{"valid", "review", "source", "operation", "failed", "registration", "missing_parents", "parent", "changed", "hash", "session", "invalid_binding", "duplicate_expected", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: strings.Repeat("b", 64)}
			parents := []v2input.Ref{ref()}
			fields := map[string]any{"schema_version": 1, "status": "pass", "operation": "render.compose", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{binding}, "registrations": []v2input.Ref{}, "parent_reports": parents}
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			ready := ReadySigningExport{Tenant: uuid.New(), Project: uuid.New(), ExportID: uuid.New()}
			switch mode {
			case "review":
				fields["status"] = "review"
			case "source":
				other := binding
				other.AssetVersionID = uuid.New()
				fields["sources"] = []v2input.SessionVersionBinding{other}
			case "operation":
				fields["operation"] = "layer.compose"
			case "failed":
				fields["status"] = "fail"
			case "registration":
				fields["registrations"] = []v2input.Ref{ref()}
			case "missing_parents":
				delete(fields, "parent_reports")
			case "parent":
				fields["parent_reports"] = []v2input.Ref{ref()}
			case "session":
				who.SessionID = uuid.Nil
			case "invalid_binding":
				binding.AssetVersionID = uuid.Nil
			case "duplicate_expected":
				parents = append(parents, parents[0])
			}
			raw, reference := contractBytes(t, fields)
			if mode == "hash" {
				reference.SHA256 = strings.Repeat("0", 64)
			}
			task := CanonicalSigningTask{Task: uuid.New(), Run: uuid.New(), Result: uuid.New(), Attempt: 1, Fence: 1, Operation: "render.compose", Version: "1", InputDigest: strings.Repeat("c", 64), ResultDigest: strings.Repeat("d", 64), Params: ref(), Inputs: json.RawMessage(`[]`), Outputs: json.RawMessage(`[]`), Metrics: json.RawMessage(`{}`)}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls := 0
			documents := signingDocumentsFunc(func(_ context.Context, _ auth.Identity, _ ReadySigningExport, _ CanonicalSigningTask, r v2input.Ref, maximum int) ([]byte, error) {
				calls++
				if r != reference || maximum != 1<<20 {
					t.Fatal("report scope changed")
				}
				if mode == "changed" && calls == 2 {
					return []byte(`{}`), nil
				}
				return raw, nil
			})
			report, err := ReadSigningRenderReport(ctx, documents, who, ready, task, binding, reference, parents)
			if mode == "valid" || mode == "review" {
				if err != nil || calls != 2 || report.Sources[0] != binding {
					t.Fatal("valid report refused", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid report evidence accepted")
			}
			if (mode == "session" || mode == "invalid_binding" || mode == "duplicate_expected" || mode == "cancelled") && calls != 0 {
				t.Fatal("invalid context read object")
			}
		})
	}
}
