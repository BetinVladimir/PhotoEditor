package interchange

import (
	"bytes"
	"context"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type ImportArchiveDocumentReader interface {
	ReadImportArchiveDocument(context.Context, v2input.Ref, int64) ([]byte, error)
}
type ImportRecipeSourceQualifier interface {
	QualifyImportRecipeSource(context.Context, uuid.UUID, v2input.SessionVersionBinding, v2input.SessionVersionBinding) error
}

// Implementations recursively remap every UUID/hash, qualify typed geometry,
// arrays and operator locks, and publish dependencies before returning a ref.
// They must reject unportable dependencies with ImportRecipeUnsupported.
type ImportRecipeDependencyRemapper interface {
	RemapImportRecipeDependency(context.Context, uuid.UUID, v2input.Ref, string, v2input.SessionVersionBinding, v2input.SessionVersionBinding) (v2input.Ref, error)
}
type ImportPreparedRecipePublisher interface {
	PublishPreparedImportRecipe(context.Context, uuid.UUID, v2input.Ref, []byte) (v2input.Ref, error)
}
type ImportPreparedRecipeQualifier interface {
	QualifyPreparedImportRecipe(context.Context, v2input.Ref, []byte, v2input.SessionVersionBinding) error
}

type PreparedImportRecipeResolver struct {
	workflow     uuid.UUID
	recipes      map[uuid.UUID]v2input.Ref
	Reader       ImportArchiveDocumentReader
	Sources      ImportRecipeSourceQualifier
	Dependencies ImportRecipeDependencyRemapper
	Qualifier    ImportPreparedRecipeQualifier
	Publisher    ImportPreparedRecipePublisher
}

func NewPreparedImportRecipeResolver(workflow uuid.UUID, parsedRaw []byte, reader ImportArchiveDocumentReader, sources ImportRecipeSourceQualifier, dependencies ImportRecipeDependencyRemapper, qualifier ImportPreparedRecipeQualifier, publisher ImportPreparedRecipePublisher) (*PreparedImportRecipeResolver, error) {
	if workflow == uuid.Nil || reader == nil || sources == nil || dependencies == nil || qualifier == nil || publisher == nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateParsedImportDocument(context.Background(), parsedRaw); err != nil {
		return nil, err
	}
	var parsed importParsedDocument
	if err := decodeImportDocument(parsedRaw, &parsed); err != nil {
		return nil, err
	}
	files := map[string]importParsedFile{}
	for _, file := range parsed.Files {
		files[file.Path] = file
	}
	recipes := map[uuid.UUID]v2input.Ref{}
	for _, entry := range parsed.Manifest.Entries {
		if entry.RecipePath != nil {
			file := files[*entry.RecipePath]
			if file.Contract != "EditingRecipeV3" || file.Content.SHA256 != *entry.RecipeSHA {
				return nil, v2input.ErrInvalid
			}
			recipes[entry.ID] = file.Content
		}
	}
	return &PreparedImportRecipeResolver{workflow: workflow, recipes: recipes, Reader: reader, Sources: sources, Dependencies: dependencies, Qualifier: qualifier, Publisher: publisher}, nil
}
func (r *PreparedImportRecipeResolver) ResolvePreparedImportRecipe(ctx context.Context, entry uuid.UUID, binding v2input.SessionVersionBinding) (*v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.Reader == nil || r.Sources == nil || r.Dependencies == nil || r.Qualifier == nil || r.Publisher == nil || binding.AssetVersionID == uuid.Nil || !validImportRef(binding.Source) || !validImportRef(v2input.Ref{ArtifactID: binding.AssetVersionID, SHA256: binding.CanvasHash}) {
		return nil, v2input.ErrInvalid
	}
	archived, ok := r.recipes[entry]
	if !ok {
		return nil, v2input.ErrInvalid
	}
	raw, err := r.Reader.ReadImportArchiveDocument(ctx, archived, 1<<20)
	if err != nil {
		return nil, err
	}
	raw = append([]byte(nil), raw...)
	if len(raw) < 2 || len(raw) > 1<<20 || rawSHA(raw) != archived.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, err
	}
	var old v2input.SessionVersionBinding
	if err := json.Unmarshal(fields["source"], &old); err != nil {
		return nil, err
	}
	if err := v2input.ValidateEditingRecipeV3(raw, old); err != nil {
		return nil, err
	}
	var overrides []v2input.Ref
	if err := json.Unmarshal(fields["overrides"], &overrides); err != nil {
		return nil, err
	}
	ctx = withImportRecipeConfirmationScope(ctx, r.workflow, entry, archived, overrides)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := r.Sources.QualifyImportRecipeSource(ctx, entry, old, binding); err != nil {
		return nil, err
	}
	// Foreign learning state is never copied or silently removed. A future
	// parameter resolver must materialize its qualified values before this step.
	if string(fields["learning_version_id"]) != "null" {
		return nil, &ImportRecipeUnsupported{Reason: "foreign_learning_dependency"}
	}
	remap := func(ref v2input.Ref, contract string) (v2input.Ref, error) {
		if err := ctx.Err(); err != nil {
			return v2input.Ref{}, err
		}
		result, err := r.Dependencies.RemapImportRecipeDependency(ctx, entry, ref, contract, old, binding)
		if err != nil {
			return v2input.Ref{}, err
		}
		if !validImportRef(result) || result.ArtifactID == ref.ArtifactID {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return result, ctx.Err()
	}
	for _, field := range []struct{ name, contract string }{{"base_style", "StyleV2"}, {"session_corrections", "CorrectionPlan"}, {"layer_stack", "LayerStack"}, {"protected_mask", "MaskRef"}} {
		value, present := fields[field.name]
		if !present || string(value) == "null" {
			continue
		}
		var ref v2input.Ref
		if err := json.Unmarshal(value, &ref); err != nil {
			return nil, err
		}
		mapped, err := remap(ref, field.contract)
		if err != nil {
			return nil, err
		}
		encoded, err := canonical.Marshal(mapped)
		if err != nil {
			return nil, err
		}
		fields[field.name] = encoded
	}
	for i, ref := range overrides {
		mapped, err := remap(ref, "Override")
		if err != nil {
			return nil, err
		}
		overrides[i] = mapped
	}
	fields["overrides"], err = canonical.Marshal(overrides)
	if err != nil {
		return nil, err
	}
	fields["source"], err = canonical.Marshal(binding)
	if err != nil {
		return nil, err
	}
	prepared, err := canonical.Marshal(fields)
	if err != nil {
		return nil, err
	}
	if err := v2input.ValidateEditingRecipeV3(prepared, binding); err != nil {
		return nil, err
	}
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(r.workflow, []byte("interchange:recipe:"+entry.String())), SHA256: rawSHA(prepared)}
	if err := r.Qualifier.QualifyPreparedImportRecipe(ctx, ref, append([]byte(nil), prepared...), binding); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	actual, err := r.Publisher.PublishPreparedImportRecipe(ctx, entry, ref, append([]byte(nil), prepared...))
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if actual != ref {
		return nil, v2input.ErrInvalid
	}
	return &ref, nil
}

// Archive bytes retain application/octet-stream until typed remapping. The
// catalogue authorizes the exact parsed file, both before and after object IO.
type BoundedImportArchiveReader struct {
	Catalog ImportDocumentCatalog
	Objects ImportDocumentObjects
}

func (r BoundedImportArchiveReader) ReadImportArchiveDocument(ctx context.Context, ref v2input.Ref, limit int64) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r.Catalog == nil || r.Objects == nil || !validImportRef(ref) || limit < 2 || limit > 8<<20 {
		return nil, v2input.ErrInvalid
	}
	entry := v2input.Entry{Ref: ref, Contract: "Artifact"}
	object, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return nil, err
	}
	if object.Key == "" || object.VersionID == "" || object.VersionID == "null" || object.SHA256 != ref.SHA256 || object.ContentType != "application/octet-stream" || object.Bytes < 2 || object.Bytes > limit {
		return nil, v2input.ErrInvalid
	}
	reader, err := r.Objects.Open(ctx, object)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, reader}, object.Bytes+1))
	closeErr := reader.Close()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(raw)) != object.Bytes || rawSHA(raw) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	current, err := r.Catalog.LookupImportDocument(ctx, entry)
	if err != nil {
		return nil, err
	}
	if current != object {
		return nil, v2input.ErrInvalid
	}
	return raw, ctx.Err()
}
