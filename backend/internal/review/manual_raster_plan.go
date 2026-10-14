package review

import (
	"context"
	"errors"
	"photoeditor/backend/internal/canonical"
	"sort"
)

// ManualRasterPlan owns the exact validated vector specification. It does not
// certify rasterized outputs, catalog authority or final processing admission.
type ManualRasterPlan struct {
	body    []byte
	canvas  Canvas
	shapes  int
	vectors manualVectors
}

// CanonicalVectors returns an independent snapshot; callers cannot mutate a plan.
func (p ManualRasterPlan) CanonicalVectors() []byte { return append([]byte(nil), p.body...) }
func (p ManualRasterPlan) Canvas() Canvas           { return p.canvas }
func (p ManualRasterPlan) ShapeCount() int          { return p.shapes }

// ResolveManualRasterPlan derives vectors from original producer Params and
// manifest. externalBody is allowed only for the exact ref declared in Params.
func ResolveManualRasterPlan(ctx context.Context, paramsBody []byte, paramsRef ArtifactRef, rootBody []byte, rootRef ArtifactRef, expected ProtectionMask, source DefectSource, externalBody []byte) (ManualRasterPlan, error) {
	empty := ManualRasterPlan{}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	external, err := VerifyManualMaskParams(paramsBody, paramsRef, rootBody, rootRef, expected, source)
	if err != nil {
		return empty, err
	}
	var vectors manualVectors
	if external != nil {
		if err := VerifyManualVectorsDocument(externalBody, *external, source); err != nil {
			return empty, err
		}
		if err := decodeExact(externalBody, &vectors); err != nil {
			return empty, err
		}
	} else {
		if len(externalBody) != 0 {
			return empty, errors.New("unexpected external manual vectors")
		}
		var params manualRasterParams
		if err := decodeExact(paramsBody, &params); err != nil {
			return empty, err
		}
		vectors = *params.Inline
	}
	body, err := canonical.Marshal(vectors)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	sort.Slice(vectors.Shapes, func(i, j int) bool { return vectors.Shapes[i].Sequence < vectors.Shapes[j].Sequence })
	return ManualRasterPlan{body: body, canvas: vectors.Canvas, shapes: len(vectors.Shapes), vectors: vectors}, nil
}
