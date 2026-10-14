package v2render

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type PerspectivePreviewSourceReader interface {
	Resolve(context.Context, uuid.UUID, uuid.UUID) (PerspectivePreviewSource, error)
}

// PerspectivePreviewBundle owns verified metadata. Pixel arrays and dense fields
// still need separate original-object validation before task admission.
type PerspectivePreviewBundle struct {
	pin       PerspectivePreviewSource
	result    v2draft.ImageResultDocument
	documents map[string][]byte
}

func (b PerspectivePreviewBundle) Source() PerspectivePreviewSource    { return b.pin }
func (b PerspectivePreviewBundle) Result() v2draft.ImageResultDocument { return b.result }
func (b PerspectivePreviewBundle) Documents() map[string][]byte {
	out := make(map[string][]byte, len(b.documents))
	for id, raw := range b.documents {
		out[id] = bytes.Clone(raw)
	}
	return out
}

func LoadPerspectivePreviewBundle(ctx context.Context, sources PerspectivePreviewSourceReader, catalog PerspectivePreviewObjectCatalog, objects v2input.SessionAnalysisObjectReader, tenant, producer uuid.UUID) (PerspectivePreviewBundle, error) {
	var zero PerspectivePreviewBundle
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if sources == nil || catalog == nil || objects == nil || tenant == uuid.Nil || producer == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	pin, err := sources.Resolve(ctx, tenant, producer)
	if err != nil {
		return zero, err
	}
	if err = validatePerspectivePreviewSource(pin); err != nil {
		return zero, err
	}
	if pin.Tenant != tenant || pin.Producer != producer {
		return zero, v2input.ErrInvalid
	}
	docs := map[string][]byte{}
	seen := map[uuid.UUID]v2draft.LayerRef{}
	total := 0
	read := func(node v2draft.LayerRef) ([]byte, error) {
		if prior, ok := seen[node.Ref.ArtifactID]; ok {
			if prior.Ref != node.Ref || prior.Contract != node.Contract || prior.Path != node.Path {
				return nil, v2input.ErrInvalid
			}
			return bytes.Clone(docs[node.Ref.ArtifactID.String()]), nil
		}
		raw, err := ReadPerspectivePreviewDocument(ctx, catalog, objects, pin, node)
		if err != nil {
			return nil, err
		}
		total += len(raw)
		if total > 4*1048576 {
			return nil, v2input.ErrInvalid
		}
		seen[node.Ref.ArtifactID] = node
		docs[node.Ref.ArtifactID.String()] = bytes.Clone(raw)
		return raw, nil
	}
	root, err := read(v2draft.LayerRef{Ref: pin.Result, Contract: "ImageResult", Path: "$"})
	if err != nil {
		return zero, err
	}
	result, err := v2draft.ParseImageResult(root, pin.Result)
	if err != nil {
		return zero, err
	}
	if result.GeneratedMask != nil {
		return zero, v2input.ErrInvalid
	}
	sourceRaw, err := read(v2draft.LayerRef{Ref: pin.Base.Source, Contract: "WorkingImage", Path: "$/source"})
	if err != nil {
		return zero, err
	}
	if result.WorkingImage != pin.Base.Source {
		if _, err = read(v2draft.LayerRef{Ref: result.WorkingImage, Contract: "WorkingImage", Path: "$/working_image"}); err != nil {
			return zero, err
		}
	}
	if _, err = read(v2draft.LayerRef{Ref: result.ChangedMask, Contract: "MaskRef", Path: "$/changed_mask"}); err != nil {
		return zero, err
	}
	reportRaw, err := read(v2draft.LayerRef{Ref: result.Report, Contract: "ImageReport", Path: "$/report"})
	if err != nil {
		return zero, err
	}
	if _, err = v2draft.DocumentChildren(reportRaw, "ImageReport", result.Report); err != nil {
		return zero, err
	}
	var report struct {
		Transform     *v2input.Ref    `json:"transform"`
		ParentReports json.RawMessage `json:"parent_reports"`
	}
	if json.Unmarshal(reportRaw, &report) != nil || report.ParentReports != nil {
		return zero, v2input.ErrInvalid
	}
	if report.Transform != nil {
		if _, err = read(v2draft.LayerRef{Ref: *report.Transform, Contract: "Artifact", Path: "$/report/transform"}); err != nil {
			return zero, err
		}
	}
	binding := AdoptedPerspectiveBinding{ResultID: pin.ResultID, Result: pin.Result, Base: pin.Base}
	if err = verifyPerspectiveResultGeometry(binding, result, sourceRaw, docs); err != nil {
		return zero, err
	}
	current, err := sources.Resolve(ctx, tenant, producer)
	if err != nil {
		return zero, err
	}
	if current != pin {
		return zero, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return PerspectivePreviewBundle{pin: pin, result: result, documents: docs}, nil
}
