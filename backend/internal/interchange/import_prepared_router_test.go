package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestPreparedImportRouterDoesNotFallbackAcrossAuthorities(t *testing.T) {
	for _, kind := range []string{"WorkingImage", "WorkingPixels", "WorkingAlpha", "MaskPixels", "DenseWarpField", "DefectLabelMap", "StyleBackgroundAsset", "StyleV2", "MaskRef", "EditingRecipeV3"} {
		t.Run(kind, func(t *testing.T) {
			entry := v2input.Entry{Contract: kind, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(kind))}}
			called := ""
			resolver := func(name string) ImportClosureNodeResolver {
				return preparedRecipeNodeFunc(func(_ context.Context, e v2input.Entry) (ImportClosureNode, error) {
					called = name
					return ImportClosureNode{Entry: e}, nil
				})
			}
			r := ImportPreparedResolverRouter{JSON: resolver("json"), Working: resolver("working"), Masks: resolver("mask"), Warps: resolver("warp"), DefectLabels: resolver("labels"), Assets: resolver("asset")}
			if _, err := r.ResolveImportNode(context.Background(), entry); err != nil {
				t.Fatal(err)
			}
			expected := "json"
			switch kind {
			case "WorkingImage", "WorkingPixels", "WorkingAlpha":
				expected = "working"
			case "MaskPixels":
				expected = "mask"
			case "DenseWarpField":
				expected = "warp"
			case "DefectLabelMap":
				expected = "labels"
			case "StyleBackgroundAsset":
				expected = "asset"
			}
			if called != expected {
				t.Fatal("authority route mismatch", called, expected)
			}
			if _, err := (ImportPreparedResolverRouter{}).ResolveImportNode(context.Background(), entry); err == nil {
				t.Fatal("missing authority accepted")
			}
		})
	}
}
func TestPreparedImportRouterCancellationBeforeAuthority(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ImportPreparedResolverRouter{}).ResolveImportNode(ctx, v2input.Entry{}); err != context.Canceled {
		t.Fatal("cancellation lost", err)
	}
}
