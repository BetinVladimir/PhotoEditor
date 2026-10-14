package provenance

import (
	"bytes"
	"context"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

// Reader must authorize each report through current adopted parent ancestry and
// immutable physical bytes; this compiler cannot turn a ref into read authority.
type SigningImageReportReader interface {
	ReadSigningImageReport(context.Context, v2input.Ref) ([]byte, error)
}
type SigningRenderReportEvidence struct {
	Reports        []v2input.Ref
	ReviewRequired bool
}

// CollectSigningRenderParentReports matches the worker's bounded report DAG:
// ordered deduplicated direct reports, exact source pins, inherited review,
// rejection of failed/unavailable ancestors, cycles, contradictory SHA and depth.
func CollectSigningRenderParentReports(ctx context.Context, reader SigningImageReportReader, roots []v2input.Ref, binding v2input.SessionVersionBinding) (SigningRenderReportEvidence, error) {
	var zero SigningRenderReportEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if reader == nil || roots == nil || len(roots) > 33 || binding.AssetVersionID == uuid.Nil || !ref(binding.Source) || !digest(binding.CanvasHash) {
		return zero, ErrInvalid
	}
	type frame struct {
		ref     v2input.Ref
		depth   int
		leaving bool
	}
	pins := map[uuid.UUID]string{}
	states := map[uuid.UUID]int{}
	heights := map[uuid.UUID]int{}
	edges := map[uuid.UUID][]v2input.Ref{}
	bodies := map[v2input.Ref][]byte{}
	direct := []v2input.Ref{}
	seen := map[uuid.UUID]bool{}
	needsReview := false
	for _, root := range roots {
		if !ref(root) {
			return zero, ErrInvalid
		}
		if !seen[root.ArtifactID] {
			seen[root.ArtifactID] = true
			direct = append(direct, root)
		}
		stack := []frame{{ref: root, depth: 1}}
		for len(stack) > 0 {
			if err := ctx.Err(); err != nil {
				return zero, err
			}
			item := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			id := item.ref.ArtifactID
			if !ref(item.ref) {
				return zero, ErrInvalid
			}
			if sha, exists := pins[id]; exists && sha != item.ref.SHA256 {
				return zero, ErrInvalid
			}
			pins[id] = item.ref.SHA256
			if item.leaving {
				height := 1
				for _, child := range edges[id] {
					if heights[child.ArtifactID]+1 > height {
						height = heights[child.ArtifactID] + 1
					}
				}
				heights[id] = height
				states[id] = 2
				continue
			}
			if item.depth > 32 || states[id] == 1 {
				return zero, ErrInvalid
			}
			if states[id] == 2 {
				if item.depth+heights[id]-1 > 32 {
					return zero, ErrInvalid
				}
				continue
			}
			if len(states) >= 1024 {
				return zero, ErrInvalid
			}
			raw, err := reader.ReadSigningImageReport(ctx, item.ref)
			if err != nil {
				return zero, err
			}
			if !pinned(raw, item.ref, 1<<20) {
				return zero, ErrInvalid
			}
			owned := append([]byte(nil), raw...)
			report, err := v2draft.ParseImageReport(owned, item.ref)
			if err != nil {
				return zero, ErrInvalid
			}
			matches := false
			for _, source := range report.Sources {
				if source == binding {
					matches = true
				}
				if source.AssetVersionID == binding.AssetVersionID && source != binding {
					return zero, ErrInvalid
				}
			}
			if !matches || report.Status == "fail" || report.Status == "unavailable" {
				return zero, ErrInvalid
			}
			needsReview = needsReview || report.Status == "review"
			states[id] = 1
			edges[id] = append([]v2input.Ref(nil), report.ParentReports...)
			bodies[item.ref] = owned
			stack = append(stack, frame{ref: item.ref, depth: item.depth, leaving: true})
			for i := len(edges[id]) - 1; i >= 0; i-- {
				stack = append(stack, frame{ref: edges[id][i], depth: item.depth + 1})
			}
		}
	}
	for reference, body := range bodies {
		if err := ctx.Err(); err != nil {
			return zero, err
		}
		current, err := reader.ReadSigningImageReport(ctx, reference)
		if err != nil {
			return zero, err
		}
		if !bytes.Equal(current, body) {
			return zero, ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return SigningRenderReportEvidence{Reports: direct, ReviewRequired: needsReview}, nil
}
