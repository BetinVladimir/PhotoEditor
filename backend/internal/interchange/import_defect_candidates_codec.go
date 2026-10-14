package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Archived proposal provenance and target label-map geometry require scoped
// authorities. Structural validation alone cannot attest either object.
type ImportDefectCandidatesQualifier interface {
	ValidateArchivedImportDefectCandidates(context.Context, []byte, v2input.SessionVersionBinding) error
	QualifyImportedDefectCandidates(context.Context, []byte, v2input.SessionVersionBinding) error
}

type ImportDefectCandidatesCodec struct {
	Masters   ImportMasterReader
	Qualifier ImportDefectCandidatesQualifier
}

func (c ImportDefectCandidatesCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, resolve func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "DefectCandidates" {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Masters == nil || c.Qualifier == nil || resolve == nil || !validImportRef(old.Source) || !validImportRef(target.Source) || old.AssetVersionID == uuid.Nil || target.AssetVersionID == uuid.Nil || old.AssetVersionID == target.AssetVersionID {
		return nil, &ImportRecipeUnsupported{Reason: "defect_candidates_authority_not_installed"}
	}
	if old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "defect_candidates_canvas_remap_required"}
	}
	if len(raw) < 2 || len(raw) > 1<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	if _, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}); err != nil {
		return nil, err
	}
	if err := c.Qualifier.ValidateArchivedImportDefectCandidates(ctx, append([]byte(nil), raw...), old); err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var source v2input.Ref
	if err := json.Unmarshal(fields["image"], &source); err != nil || source != old.Source {
		return nil, v2input.ErrInvalid
	}
	master, err := c.Masters.ReadImportMaster(ctx, target.Source, 1<<20)
	if err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(master)
	if err != nil || rawSHA(master) != target.Source.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var archivedCanvas review.Canvas
	if err := json.Unmarshal(fields["canvas"], &archivedCanvas); err != nil || archivedCanvas != image.Canvas {
		return nil, &ImportRecipeUnsupported{Reason: "defect_candidates_geometry_changed"}
	}
	archivedChain, err := canonical.Bytes(fields["transform_chain"])
	if err != nil {
		return nil, err
	}
	targetChain, err := canonical.Bytes(image.TransformChain)
	if err != nil || !bytes.Equal(archivedChain, targetChain) {
		return nil, &ImportRecipeUnsupported{Reason: "defect_candidates_geometry_changed"}
	}
	var archivedPixels v2input.Ref
	if err := json.Unmarshal(fields["source_pixels"], &archivedPixels); err != nil || !validImportRef(archivedPixels) {
		return nil, v2input.ErrInvalid
	}
	var oldLabels v2input.Ref
	if err := json.Unmarshal(fields["label_map"], &oldLabels); err != nil || !validImportRef(oldLabels) {
		return nil, v2input.ErrInvalid
	}
	labels, err := resolve(oldLabels, "DefectLabelMap")
	if err != nil {
		return nil, err
	}
	if !validImportRef(labels) || labels.ArtifactID == oldLabels.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	fields["image"], err = canonical.Marshal(target.Source)
	if err != nil {
		return nil, err
	}
	fields["source_pixels"], err = canonical.Marshal(image.Pixels)
	if err != nil {
		return nil, err
	}
	fields["label_map"], err = canonical.Marshal(labels)
	if err != nil {
		return nil, err
	}
	var candidates []map[string]json.RawMessage
	if err := json.Unmarshal(fields["candidates"], &candidates); err != nil || candidates == nil || len(candidates) > 4096 {
		return nil, v2input.ErrInvalid
	}
	for _, item := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var label int
		var diameter float64
		if err := json.Unmarshal(item["label"], &label); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(item["diameter"], &diameter); err != nil {
			return nil, err
		}
		id, err := canonical.Hash(struct {
			Version  string          `json:"version"`
			Source   string          `json:"source_sha256"`
			Mask     string          `json:"mask_sha256"`
			Label    int             `json:"label"`
			BBox     json.RawMessage `json:"bbox"`
			Diameter float64         `json:"diameter"`
		}{"defect_candidate_v1", image.Pixels.SHA256, labels.SHA256, label, item["bbox"], diameter})
		if err != nil {
			return nil, err
		}
		item["id"], err = canonical.Marshal(id)
		if err != nil {
			return nil, err
		}
	}
	fields["candidates"], err = canonical.Marshal(candidates)
	if err != nil {
		return nil, err
	}
	prepared, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := c.QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func (c ImportDefectCandidatesCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "DefectCandidates" {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if c.Masters == nil || c.Qualifier == nil || !validImportRef(target.Source) {
		return &ImportRecipeUnsupported{Reason: "defect_candidates_authority_not_installed"}
	}
	if _, err := review.ParseDefectProposalStructure(ctx, raw, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(raw)}); err != nil {
		return err
	}
	master, err := c.Masters.ReadImportMaster(ctx, target.Source, 1<<20)
	if err != nil {
		return err
	}
	image, err := review.ParseWorkingImage(master)
	if err != nil || rawSHA(master) != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	var fields struct {
		Image        v2input.Ref     `json:"image"`
		SourcePixels v2input.Ref     `json:"source_pixels"`
		Canvas       review.Canvas   `json:"canvas"`
		Chain        json.RawMessage `json:"transform_chain"`
	}
	if err := json.Unmarshal(raw, &fields); err != nil || fields.Image != target.Source || fields.SourcePixels.ArtifactID != image.Pixels.ID || fields.SourcePixels.SHA256 != image.Pixels.SHA256 || fields.Canvas != image.Canvas {
		return v2input.ErrInvalid
	}
	chain, err := canonical.Bytes(fields.Chain)
	if err != nil {
		return err
	}
	actual, err := canonical.Bytes(image.TransformChain)
	if err != nil || !bytes.Equal(chain, actual) {
		return v2input.ErrInvalid
	}
	return c.Qualifier.QualifyImportedDefectCandidates(ctx, append([]byte(nil), raw...), target)
}
