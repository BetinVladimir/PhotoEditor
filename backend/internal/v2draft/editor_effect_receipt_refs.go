package v2draft

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// DecodeEditorExecutedBaseReceipt validates immutable wire bytes and every
// source pin. Shape validation is deliberately separate from live SQL authority
// and independent seal adoption: neither a reference nor this parser grants use.
func DecodeEditorExecutedBaseReceipt(raw []byte) (EditorExecutedBaseReceipt, error) {
	var value EditorExecutedBaseReceipt
	invalid := func() (EditorExecutedBaseReceipt, error) { return EditorExecutedBaseReceipt{}, v2input.ErrInvalid }
	if len(raw) < 2 || len(raw) > 1048576 || json.Unmarshal(raw, &value) != nil {
		return invalid()
	}
	wire, err := canonical.Marshal(value)
	if err != nil || !bytes.Equal(wire, raw) || value.SchemaVersion != 1 || value.BuildRunID == uuid.Nil {
		return invalid()
	}
	for _, ref := range []v2input.Ref{value.OriginalRecipe, value.OriginalSource.Source, value.ProcessedSource.Source, value.ExecutedStyle, value.Quality, value.ImageResult} {
		if !isDocumentRef(ref) { return invalid() }
	}
	if value.OriginalSource.AssetVersionID == uuid.Nil || value.ProcessedSource.AssetVersionID != value.OriginalSource.AssetVersionID ||
		!correctionDigest(value.OriginalSource.CanvasHash) || !correctionDigest(value.ProcessedSource.CanvasHash) ||
		!correctionDigest(value.CompilationSHA) || !correctionDigest(value.TraceSHA) {
		return invalid()
	}
	return value, nil
}

func editorExecutedReceiptRefs(raw []byte, path string, parent v2input.Ref) ([]LayerRef, error) {
	value, err := DecodeEditorExecutedBaseReceipt(raw)
	if err != nil || !isDocumentRef(parent) { return nil, v2input.ErrInvalid }
	children := []LayerRef{}
	add := func(suffix, contract string, ref v2input.Ref) {
		children = append(children, LayerRef{Path: path + suffix, Contract: contract, Ref: ref, ParentRef: parent})
	}
	// Original geometry belongs to provenance. Generic current-source traversal
	// must not substitute the processed binding for the original frozen recipe.
	add(".original_recipe", "FrozenRenderRecipe", value.OriginalRecipe)
	add(".original_source.source", "WorkingImage", value.OriginalSource.Source)
	children[len(children)-1].CanvasHash = value.OriginalSource.CanvasHash
	add(".processed_source.source", "WorkingImage", value.ProcessedSource.Source)
	children[len(children)-1].CanvasHash = value.ProcessedSource.CanvasHash
	add(".executed_style", "StyleV2", value.ExecutedStyle)
	add(".quality", "QualityReport", value.Quality)
	add(".image_result", "ImageResult", value.ImageResult)
	return children, nil
}
