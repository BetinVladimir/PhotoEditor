package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"testing"
)

// Tests role projection only; actual Override/Style schemas are qualified by
// the mandatory codec layer before this structural adapter in production.
func TestPreparedSchemaChildrenProjectsInlineMaskAndRejectsOpaqueRefs(t *testing.T) {
	_, _, imageRaw := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(imageRaw)
	if err != nil {
		t.Fatal(err)
	}
	pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("pixels"))}
	mask := map[string]any{"artifact": pixels, "canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain), "selector": "manual", "confidence": 1, "protected": true}
	for _, mode := range []string{"override_mask", "mask", "style_leaf", "style_ref", "override_ref", "params_ref", "unknown", "hash", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			kind := "Override"
			value := any(map[string]any{"mask_refs": []any{map[string]any{"mask": mask}}})
			switch mode {
			case "mask":
				kind = "MaskRef"
				value = mask
			case "style_leaf":
				kind = "StyleV2"
				value = map[string]any{"extensions": map[string]any{}}
			case "style_ref":
				kind = "StyleV2"
				value = map[string]any{"unknown": pixels}
			case "override_ref":
				value = map[string]any{"unknown": pixels}
			case "params_ref":
				kind = "Params"
				value = map[string]any{"unknown": pixels}
			case "unknown":
				kind = "Unknown"
			}
			raw, err := canonical.Marshal(value)
			if err != nil {
				t.Fatal(err)
			}
			entry := v2input.Entry{Contract: kind, Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "hash" {
				entry.Ref.SHA256 = rawSHA([]byte("foreign"))
			}
			if mode == "cancel" {
				cancel()
			}
			children, err := (ImportPreparedSchemaChildren{}).ImportDocumentChildren(ctx, entry, raw)
			switch mode {
			case "override_mask", "mask":
				if err != nil || len(children) != 1 || children[0] != (v2input.Entry{Ref: pixels, Contract: "MaskPixels"}) {
					t.Fatal("typed mask child", children, err)
				}
			case "style_leaf":
				if err != nil || len(children) != 0 {
					t.Fatal("style leaf", err)
				}
			default:
				if err == nil {
					t.Fatal("untyped dependency accepted")
				}
			}
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		})
	}
}

func TestPreparedSchemaChildrenProjectsStatsAndResolvedRoles(t *testing.T) {
	source := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	statsRaw, err := canonical.Marshal(map[string]any{"source_image": source, "feature_version": "stats20_v1", "values": make([]float64, 20), "coverage_bins": make([]bool, 512), "mask_confidence": 1, "warnings": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	statsEntry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(statsRaw)}, Contract: "Stats20"}
	children, err := (ImportPreparedSchemaChildren{}).ImportDocumentChildren(context.Background(), statsEntry, statsRaw)
	if err != nil || len(children) != 1 || children[0] != (v2input.Entry{Ref: source, Contract: "WorkingImage"}) {
		t.Fatal("stats source role", children, err)
	}
	resolvedRaw, err := canonical.Marshal(map[string]any{"params": map[string]any{"image": source, "stats": statsEntry.Ref}})
	if err != nil {
		t.Fatal(err)
	}
	resolvedEntry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(resolvedRaw)}, Contract: "ResolvedStyle"}
	children, err = (ImportPreparedSchemaChildren{}).ImportDocumentChildren(context.Background(), resolvedEntry, resolvedRaw)
	if err != nil || len(children) != 2 {
		t.Fatal("resolved child roles", children, err)
	}
	roles := map[string]v2input.Ref{}
	for _, child := range children {
		roles[child.Contract] = child.Ref
	}
	if roles["WorkingImage"] != source || roles["Stats20"] != statsEntry.Ref {
		t.Fatal("resolved child relabel", children)
	}
}

func TestPreparedSchemaChildrenIncludesInlineDustReviewProposal(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("image"))}
	proposal := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("dust proposal"))}
	selection := review.DustSelection{
		Version:  "sensor_dust_review_v1",
		Proposal: review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256},
		Base:     review.ArtifactRef{ID: image.ArtifactID, SHA256: image.SHA256},
		Revision: 1,
		Accepted: []string{},
		Rejected: []string{},
	}
	raw, err := canonical.Marshal(importDustRepairParams{Image: image, Proposal: &proposal, Review: &selection})
	if err != nil {
		t.Fatal(err)
	}
	entry := v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}, Contract: "Params"}
	children, err := (ImportPreparedSchemaChildren{}).ImportDocumentChildren(context.Background(), entry, raw)
	if err != nil || len(children) != 2 || children[0] != (v2input.Entry{Ref: image, Contract: "WorkingImage"}) || children[1] != (v2input.Entry{Ref: proposal, Contract: "DustCandidates"}) {
		t.Fatal("inline dust review proposal absent from activation closure", children, err)
	}
}
