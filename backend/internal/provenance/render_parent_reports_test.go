package provenance

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

type signingReportReaderFunc func(context.Context, v2input.Ref) ([]byte, error)

func (f signingReportReaderFunc) ReadSigningImageReport(ctx context.Context, r v2input.Ref) ([]byte, error) {
	return f(ctx, r)
}

func TestSigningParentReportDAGPreservesReviewAndSourcePins(t *testing.T) {
	for _, mode := range []string{"valid", "review_ancestor", "failed_ancestor", "source", "changed", "contradictory_root", "empty", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
			docs := map[v2input.Ref][]byte{}
			put := func(status string, parents []v2input.Ref) v2input.Ref {
				source := binding
				if mode == "source" {
					source.CanvasHash = strings.Repeat("c", 64)
				}
				raw, ref := contractBytes(t, map[string]any{"schema_version": 1, "status": status, "operation": "render.compose", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{source}, "registrations": []v2input.Ref{}, "parent_reports": parents})
				docs[ref] = raw
				return ref
			}
			status := "pass"
			if mode == "review_ancestor" {
				status = "review"
			}
			if mode == "failed_ancestor" {
				status = "fail"
			}
			ancestor := put(status, []v2input.Ref{})
			root := put("pass", []v2input.Ref{ancestor})
			roots := []v2input.Ref{root, root}
			if mode == "empty" {
				roots = []v2input.Ref{}
			}
			if mode == "contradictory_root" {
				roots[1].SHA256 = strings.Repeat("d", 64)
			}
			reads := map[v2input.Ref]int{}
			reader := signingReportReaderFunc(func(_ context.Context, reference v2input.Ref) ([]byte, error) {
				reads[reference]++
				if mode == "changed" && reads[reference] > 1 {
					return []byte(`{}`), nil
				}
				return docs[reference], nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			evidence, err := CollectSigningRenderParentReports(ctx, reader, roots, binding)
			if mode == "valid" || mode == "review_ancestor" || mode == "empty" {
				count := 1
				if mode == "empty" {
					count = 0
				}
				if err != nil || len(evidence.Reports) != count || evidence.ReviewRequired != (mode == "review_ancestor") {
					t.Fatal("invalid report evidence", err)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid report DAG accepted")
			}
		})
	}
}

// A shared subtree may have been accepted at a shallower depth first. Its
// cached height must still count when reached through a later, deeper root.
func TestSigningParentReportDepthIncludesPreviouslyVisitedSubtree(t *testing.T) {
	for _, depth := range []int{32, 33} {
		t.Run(string(rune('A'+depth-32)), func(t *testing.T) {
			binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, CanvasHash: strings.Repeat("b", 64)}
			documents := map[v2input.Ref][]byte{}
			put := func(parents []v2input.Ref) v2input.Ref {
				raw, reference := contractBytes(t, map[string]any{"schema_version": 1, "status": "pass", "operation": "render.compose", "skipped_count": 0, "reason": nil, "outside_mask_max_error": 0, "clipping_fraction": 0, "identity": nil, "transform": nil, "sources": []v2input.SessionVersionBinding{binding}, "registrations": []v2input.Ref{}, "parent_reports": parents})
				documents[reference] = raw
				return reference
			}
			leaf := put([]v2input.Ref{})
			shared := put([]v2input.Ref{leaf})
			root := shared
			for n := 2; n < depth; n++ {
				root = put([]v2input.Ref{root})
			}
			reads := map[v2input.Ref]int{}
			reader := signingReportReaderFunc(func(_ context.Context, reference v2input.Ref) ([]byte, error) {
				reads[reference]++
				return append([]byte(nil), documents[reference]...), nil
			})
			evidence, err := CollectSigningRenderParentReports(context.Background(), reader, []v2input.Ref{shared, root, shared}, binding)
			if depth == 33 {
				if err == nil || evidence.Reports != nil {
					t.Fatal("over-depth reused subtree accepted")
				}
				return
			}
			if err != nil || len(evidence.Reports) != 2 || evidence.Reports[0] != shared || evidence.Reports[1] != root {
				t.Fatal("valid boundary graph rejected or order lost", err)
			}
			for reference := range documents {
				if reads[reference] != 2 {
					t.Fatal("report not read once and rechecked once", reference, reads[reference])
				}
			}
		})
	}
}
