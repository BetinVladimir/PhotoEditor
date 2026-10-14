package interchange

import (
	"bytes"
	"context"
	"encoding/json"

	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// Stats20 is an immutable measurement. Source identity changes on import;
// measurement values, coverage and warnings are preserved exactly.
type ImportStatsDocumentCodec struct{}

func (ImportStatsDocumentCodec) RemapImportDocument(ctx context.Context, kind string, raw []byte, old, target v2input.SessionVersionBinding, _ func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if kind != "Stats20" {
		return nil, &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if !validImportRef(old.Source) || !validImportRef(target.Source) || old.CanvasHash != target.CanvasHash {
		return nil, &ImportRecipeUnsupported{Reason: "stats_source_remap_required"}
	}
	if len(raw) < 2 || len(raw) > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	archived, err := review.ParseStats20(raw)
	if err != nil || archived.SourceImage == nil || archived.SourceImage.ID != old.Source.ArtifactID || archived.SourceImage.SHA256 != old.Source.SHA256 {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	fields["source_image"], err = canonical.Marshal(target.Source)
	if err != nil {
		return nil, err
	}
	prepared, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := (ImportStatsDocumentCodec{}).QualifyRemappedImportDocument(ctx, kind, prepared, target); err != nil {
		return nil, err
	}
	return prepared, ctx.Err()
}

func (ImportStatsDocumentCodec) QualifyRemappedImportDocument(ctx context.Context, kind string, raw []byte, target v2input.SessionVersionBinding) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if kind != "Stats20" {
		return &ImportRecipeUnsupported{Reason: "dependency_contract_not_installed"}
	}
	if !validImportRef(target.Source) || len(raw) < 2 || len(raw) > 8<<20 {
		return v2input.ErrInvalid
	}
	stats, err := review.ParseStats20(raw)
	if err != nil || stats.SourceImage == nil || stats.SourceImage.ID != target.Source.ArtifactID || stats.SourceImage.SHA256 != target.Source.SHA256 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}
