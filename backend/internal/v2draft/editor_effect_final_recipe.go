package v2draft

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type EditorExecutedBaseReceipt struct {
	SchemaVersion       int                           `json:"schema_version"`
	BuildRunID          uuid.UUID                     `json:"build_run_id"`
	OriginalRecipe      v2input.Ref                   `json:"original_recipe"`
	OriginalSource      v2input.SessionVersionBinding `json:"original_source"`
	ProcessedSource     v2input.SessionVersionBinding `json:"processed_source"`
	ExecutedStyle       v2input.Ref                   `json:"executed_style"`
	CompilationSHA      string                        `json:"compilation_sha"`
	TraceSHA            string                        `json:"trace_sha"`
	Quality             v2input.Ref                   `json:"quality"`
	ImageResult         v2input.Ref                   `json:"image_result"`
	GenerativeRequested bool                          `json:"generative_requested"`
	InheritedReview     bool                          `json:"inherited_review"`
}

// BuildEditorFinalRecipe binds a physically read, independently adopted receipt
// to the original freeze. The caller must also prove the exact sealed SQL row.
// Authored layers ran before Apply; this identity compose cannot execute them,
// the Style, learning/session corrections or scalar deltas a second time.
func BuildEditorFinalRecipe(originalRaw []byte, originalRef, receiptRef v2input.Ref, receiptRaw []byte, build uuid.UUID) ([]byte, error) {
	invalid := func() ([]byte, error) { return nil, v2input.ErrInvalid }
	if !isDocumentRef(originalRef) || !isDocumentRef(receiptRef) || build == uuid.Nil {
		return invalid()
	}
	for _, item := range []struct {
		raw []byte
		ref v2input.Ref
	}{{originalRaw, originalRef}, {receiptRaw, receiptRef}} {
		sum := sha256.Sum256(item.raw)
		wire, err := canonical.Bytes(item.raw)
		if err != nil || !bytes.Equal(wire, item.raw) || hex.EncodeToString(sum[:]) != item.ref.SHA256 {
			return invalid()
		}
	}
	original, err := DecodeFrozenRenderRecipe(originalRaw)
	if err != nil {
		return invalid()
	}
	var authored map[string]json.RawMessage
	if json.Unmarshal(original.Recipe, &authored) != nil || authored["executed_base_receipt"] != nil {
		return invalid()
	}
	receipt, err := DecodeEditorExecutedBaseReceipt(receiptRaw)
	if err != nil || receipt.BuildRunID != build || receipt.OriginalRecipe != originalRef || receipt.OriginalSource != original.Scalars.Source {
		return invalid()
	}
	return CompileUnsealedEditorFinalIdentity(originalRaw, originalRef, receipt.ProcessedSource, receipt.ExecutedStyle, receiptRef)
}

// CompileUnsealedEditorFinalIdentity is the lower pure identity compiler shared
// by the guarded service entry and operator-only measurement. It does not attest
// an adopted receipt or sealed SQL row. Neither its output nor a local receipt
// may be passed as production admission authority; the service retains every
// caller/catalog/receipt/SQL check before publication.
func CompileUnsealedEditorFinalIdentity(originalRaw []byte, originalRef v2input.Ref, processedSource v2input.SessionVersionBinding, executedStyle, receiptRef v2input.Ref) ([]byte, error) {
	invalid := func() ([]byte, error) { return nil, v2input.ErrInvalid }
	if !isDocumentRef(originalRef) || !isDocumentRef(receiptRef) || !isDocumentRef(executedStyle) {
		return invalid()
	}
	sum := sha256.Sum256(originalRaw)
	wire, err := canonical.Bytes(originalRaw)
	if err != nil || !bytes.Equal(wire, originalRaw) || hex.EncodeToString(sum[:]) != originalRef.SHA256 {
		return invalid()
	}
	original, err := DecodeFrozenRenderRecipe(originalRaw)
	if err != nil {
		return invalid()
	}
	var authored map[string]json.RawMessage
	if json.Unmarshal(original.Recipe, &authored) != nil || authored["executed_base_receipt"] != nil {
		return invalid()
	}
	recipe, err := canonical.Marshal(map[string]any{"schema_version": 3, "source": processedSource, "base_style": executedStyle, "session_corrections": nil, "learning_version_id": nil, "overrides": []v2input.Ref{}, "layer_stack": nil, "renderer_version": "v2.2", "parameter_delta": CorrectionVector{}, "executed_base_receipt": receiptRef})
	if err != nil || v2input.ValidateExecutedEditingRecipeV3(recipe, processedSource) != nil {
		return invalid()
	}
	digest := sha256.Sum256(recipe)
	scalars := original.Scalars
	scalars.Source = processedSource
	scalars.BaseStyle = executedStyle
	scalars.EffectiveDelta = CorrectionVector{}
	frozen := FrozenRenderRecipe{SchemaVersion: 1, Recipe: recipe, RecipeSHA256: hex.EncodeToString(digest[:]), Scalars: scalars, Learning: nil}
	output, err := canonical.Marshal(frozen)
	if err != nil || len(output) > 1048576 {
		return invalid()
	}
	return output, nil
}
