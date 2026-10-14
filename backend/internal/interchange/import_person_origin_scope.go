package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// StagedImportPersonOriginScope qualifies the actual staged archive documents.
// A signed claim or matching manifest hash cannot substitute for these reads.
type StagedImportPersonOriginScope struct {
	Source  ImportPersonOriginReceiptSource
	Objects ImportDocumentObjects
}

// DiscoverEntry reads the staged authored recipe before constructing any
// expectation. Manifest refs select candidates; physical recipe/source/mask
// checks below decide whether each selected occurrence is admissible.
func (s StagedImportPersonOriginScope) DiscoverEntry(ctx context.Context, parsed []byte,
	entryID uuid.UUID, modelLockSHA string) ([]PortablePersonOriginExpectation, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if entryID == uuid.Nil || !portablePersonOriginSHA.MatchString(modelLockSHA) ||
		s.Source == nil || s.Objects == nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return nil, err
	}
	var document importParsedDocument
	if err := decodeImportDocument(parsed, &document); err != nil {
		return nil, err
	}
	selected := false
	for _, entry := range document.Manifest.Entries {
		if entry.ID == entryID {
			selected = true
			break
		}
	}
	if !selected {
		return nil, v2input.ErrInvalid
	}
	result := make([]PortablePersonOriginExpectation, 0)
	for _, origin := range document.Manifest.PersonOrigins {
		if origin.EntryID != entryID {
			continue
		}
		recipeRaw, err := s.read(ctx, parsed, document, origin.Recipe, "EditingRecipeV3", 1<<20)
		if err != nil {
			return nil, err
		}
		var recipe struct {
			Source v2input.SessionVersionBinding `json:"source"`
		}
		if err := json.Unmarshal(recipeRaw, &recipe); err != nil {
			return nil, v2input.ErrInvalid
		}
		scope := PortablePersonOriginExpectation{EntryID: entryID, Source: recipe.Source,
			Recipe: origin.Recipe, Override: origin.Override, MaskRef: origin.MaskRef,
			MaskPixels: origin.MaskPixels, ModelLockSHA: modelLockSHA}
		if err := s.QualifyImportedPersonOriginScope(ctx, parsed, scope); err != nil {
			return nil, err
		}
		result = append(result, scope)
	}
	return result, ctx.Err()
}

func (s StagedImportPersonOriginScope) read(ctx context.Context, parsed []byte, document importParsedDocument, original v2input.Ref, contract string, limit int64) ([]byte, error) {
	if s.Source == nil || s.Objects == nil || !validImportRef(original) {
		return nil, v2input.ErrInvalid
	}
	var staged v2input.Ref
	matches := 0
	for _, item := range document.Manifest.Inventory {
		if item.ID == original.ArtifactID {
			if item.SHA != original.SHA256 || item.Contract != contract {
				return nil, v2input.ErrInvalid
			}
			for _, file := range document.Files {
				if file.Path == item.Path {
					if file.Contract != contract || file.Content.SHA256 != original.SHA256 {
						return nil, v2input.ErrInvalid
					}
					staged = file.Content
					matches++
				}
			}
		}
	}
	if matches != 1 || !validImportRef(staged) {
		return nil, v2input.ErrInvalid
	}
	object, err := s.Source.LookupPersonOriginReceipt(ctx, parsed, staged)
	if err != nil {
		return nil, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != original.SHA256 || object.ContentType != "application/octet-stream" || object.Bytes < 2 || object.Bytes > limit {
		return nil, v2input.ErrInvalid
	}
	stream, err := s.Objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return nil, err
	}
	if stream == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx: ctx, reader: stream}, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != original.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return nil, v2input.ErrInvalid
	}
	current, err := s.Source.LookupPersonOriginReceipt(ctx, parsed, staged)
	if err != nil || current != object {
		return nil, v2input.ErrInvalid
	}
	return raw, ctx.Err()
}

func (s StagedImportPersonOriginScope) QualifyImportedPersonOriginScope(ctx context.Context, parsed []byte, expected PortablePersonOriginExpectation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return err
	}
	var document importParsedDocument
	if err := decodeImportDocument(parsed, &document); err != nil {
		return err
	}
	entries := 0
	for _, entry := range document.Manifest.Entries {
		if entry.ID == expected.EntryID {
			if entry.RecipePath == nil || entry.RecipeSHA == nil || *entry.RecipeSHA != expected.Recipe.SHA256 {
				return v2input.ErrInvalid
			}
			matched := false
			for _, item := range document.Manifest.Inventory {
				if item.Path == *entry.RecipePath && item.ID == expected.Recipe.ArtifactID && item.SHA == expected.Recipe.SHA256 && item.Contract == "EditingRecipeV3" {
					matched = true
				}
			}
			if !matched {
				return v2input.ErrInvalid
			}
			entries++
		}
	}
	if entries != 1 {
		return v2input.ErrInvalid
	}
	recipeRaw, err := s.read(ctx, parsed, document, expected.Recipe, "EditingRecipeV3", 1<<20)
	if err != nil {
		return err
	}
	if err := v2input.ValidateEditingRecipeV3(recipeRaw, expected.Source); err != nil {
		return err
	}
	var recipe struct {
		Source    v2input.SessionVersionBinding `json:"source"`
		Overrides []v2input.Ref                 `json:"overrides"`
	}
	if json.Unmarshal(recipeRaw, &recipe) != nil || recipe.Source != expected.Source {
		return v2input.ErrInvalid
	}
	matches := 0
	for _, ref := range recipe.Overrides {
		if ref == expected.Override {
			matches++
		}
	}
	if matches != 1 {
		return v2input.ErrInvalid
	}
	sourceRaw, err := s.read(ctx, parsed, document, expected.Source.Source, "WorkingImage", 1<<20)
	if err != nil {
		return err
	}
	image, err := review.ParseWorkingImage(sourceRaw)
	if err != nil {
		return err
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil || canvas != expected.Source.CanvasHash {
		return v2input.ErrInvalid
	}
	maskRaw, err := s.read(ctx, parsed, document, expected.MaskRef, "MaskRef", 1<<20)
	if err != nil {
		return err
	}
	mask, err := review.ParseMaskRef(maskRaw)
	if err != nil || mask.Selector != "person" || mask.Protected || mask.ObjectID != nil || mask.ModelLockID == nil || *mask.ModelLockID != expected.ModelLockSHA || mask.Canvas != image.Canvas || mask.Artifact.ID != expected.MaskPixels.ArtifactID || mask.Artifact.SHA256 != expected.MaskPixels.SHA256 {
		return v2input.ErrInvalid
	}
	maskChain, err := canonical.Bytes(mask.TransformChain)
	if err != nil {
		return err
	}
	sourceChain, err := canonical.Bytes(image.TransformChain)
	if err != nil || !bytes.Equal(maskChain, sourceChain) {
		return v2input.ErrInvalid
	}
	overrideRaw, err := s.read(ctx, parsed, document, expected.Override, "Override", 8<<20)
	if err != nil {
		return err
	}
	if err := editor.ValidateOverride(overrideRaw, image.Canvas.SourceSHA256); err != nil {
		return err
	}
	var override struct {
		Masks []struct {
			Purpose   string          `json:"purpose"`
			Confirmed bool            `json:"confirmed"`
			Mask      json.RawMessage `json:"mask"`
		} `json:"mask_refs"`
	}
	if json.Unmarshal(overrideRaw, &override) != nil {
		return v2input.ErrInvalid
	}
	matches = 0
	for _, binding := range override.Masks {
		if binding.Purpose == "person_subject" && binding.Confirmed && bytes.Equal(binding.Mask, maskRaw) {
			matches++
		}
	}
	if matches != 1 {
		return v2input.ErrInvalid
	}
	pixels := 0
	for _, item := range document.Manifest.Inventory {
		if item.ID == expected.MaskPixels.ArtifactID && item.SHA == expected.MaskPixels.SHA256 && item.Contract == "MaskPixels" {
			pixels++
		}
	}
	if pixels != 1 {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

var _ ImportPersonOriginScope = StagedImportPersonOriginScope{}
