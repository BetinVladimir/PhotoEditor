package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/workflow"
)

// Supplies complete model/geometry/source semantic authority for the installed
// document codecs. Infrastructure supplies scoped readers/publisher/warp port;
// operator authority must never replace those with request-selected storage.
type ImportEntryRecipeAuthorityFactory interface {
	BuildImportEntryRecipeAuthority(context.Context, ImportPreparedScope, []byte, ImportArchiveDocumentReader, ImportMasterReader, PreparedImportArtifactPublisher, ImportDenseWarpRemapper) (ImportDependencyCodec, ImportPreparedRecipeQualifier, error)
}
type ImportWorkflowRecipeAuthorityBinder interface {
	BindImportWorkflowRecipeAuthority(context.Context, workflow.InterchangeImportCandidate, []byte, ImportResolverDependencies) (ImportEntryRecipeAuthorityFactory, error)
}
type ImportEntryBinaryRemapperFactory interface {
	BuildImportEntryBinaryRemapper(context.Context, uuid.UUID, []byte, ImportArchiveDocumentReader, ImportRecipeSourceQualifier, ImportMasterReader, ImportDocumentCatalog, ImportDocumentObjects, ImportBinaryPublisher) (ImportBinaryRemapper, error)
}
type SQLImportSchedulerRecipeFactory struct {
	Authority ImportEntryRecipeAuthorityFactory
}
type scopedSQLImportRecipes struct {
	candidate workflow.InterchangeImportCandidate
	parsed    []byte
	deps      ImportResolverDependencies
	objects   ExportExecutionObjects
	authority ImportEntryRecipeAuthorityFactory
}

func (f SQLImportSchedulerRecipeFactory) BuildImportSchedulerRecipes(ctx context.Context, c workflow.InterchangeImportCandidate, parsedRaw []byte, deps ImportResolverDependencies) (ImportPreparedRecipeResolver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	objects, ok := deps.Objects.(ExportExecutionObjects)
	if f.Authority == nil || !ok || objects == nil || deps.DB == nil || c.Tenant == uuid.Nil || c.Run == uuid.Nil || c.Workflow == uuid.Nil || c.Project == uuid.Nil || c.Bucket != deps.Bucket || rawSHA(parsedRaw) != c.SHA || deps.MaxFileBytes < 1 || deps.MaxFileBytes > 128<<30 {
		return nil, v2input.ErrInvalid
	}
	parsed := append([]byte(nil), parsedRaw...)
	if _, err := validateParsedImportDocument(ctx, parsed); err != nil {
		return nil, err
	}
	authority := f.Authority
	if binder, ok := authority.(ImportWorkflowRecipeAuthorityBinder); ok {
		var err error
		authority, err = binder.BindImportWorkflowRecipeAuthority(ctx, c, append([]byte(nil), parsed...), deps)
		if err != nil {
			return nil, err
		}
		if authority == nil {
			return nil, v2input.ErrInvalid
		}
	}
	return &scopedSQLImportRecipes{candidate: c, parsed: parsed, deps: deps, objects: objects, authority: authority}, ctx.Err()
}
func (r *scopedSQLImportRecipes) ResolvePreparedImportRecipe(ctx context.Context, entry uuid.UUID, target v2input.SessionVersionBinding) (*v2input.Ref, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if r == nil || r.authority == nil || entry == uuid.Nil || target.AssetVersionID == uuid.Nil || !validImportRef(target.Source) || !validImportRefForSHA(target.CanvasHash) {
		return nil, v2input.ErrInvalid
	}
	c := r.candidate
	parsed := append([]byte(nil), r.parsed...)
	masters, err := NewSQLImportMasterReader(c.Tenant, c.Workflow, parsed, r.deps)
	if err != nil {
		return nil, err
	}
	catalog := SQLImportOpaqueCatalog{DB: r.deps.DB, Tenant: c.Tenant, Workflow: c.Workflow, Parsed: append([]byte(nil), parsed...), Bucket: r.deps.Bucket}
	reader := BoundedImportArchiveReader{Catalog: catalog, Objects: r.objects}
	sources, err := NewParsedImportRecipeSourceQualifier(parsed, reader, masters)
	if err != nil {
		return nil, err
	}
	scope := ImportPreparedScope{Tenant: c.Tenant, Run: c.Run, Workflow: c.Workflow, Entry: entry, Binding: target}
	store := SQLPreparedImportArtifactStore{DB: r.deps.DB, Tenant: c.Tenant, Identity: ImportDocumentIdentity{Run: c.Run, Workflow: c.Workflow}, Parsed: append([]byte(nil), parsed...), Bucket: r.deps.Bucket}
	publisher := PreparedImportArtifactPublisher{Scope: scope, Store: store, Objects: r.objects, SourceObjects: r.objects}
	warps, err := NewImportedDenseWarpRemapper(c.Workflow, entry, parsed, catalog, r.objects, masters, publisher, r.deps.MaxFileBytes)
	if err != nil {
		return nil, err
	}
	codec, qualifier, err := r.authority.BuildImportEntryRecipeAuthority(ctx, scope, append([]byte(nil), parsed...), reader, masters, publisher, warps)
	if err != nil {
		return nil, err
	}
	if codec == nil || qualifier == nil {
		return nil, v2input.ErrInvalid
	}
	remapper, err := NewRecursiveImportDependencyRemapper(c.Workflow, parsed, reader, sources, codec, publisher)
	if err != nil {
		return nil, err
	}
	remapper.Binary = ImportMaskBinaryRemapper{Workflow: c.Workflow, Catalog: catalog, Objects: r.objects, Masters: masters, Publisher: publisher}
	if binaryFactory, ok := r.authority.(ImportEntryBinaryRemapperFactory); ok {
		installed, err := binaryFactory.BuildImportEntryBinaryRemapper(ctx, c.Workflow, parsed, reader, sources, masters, catalog, r.objects, publisher)
		if err != nil {
			return nil, err
		}
		if installed == nil {
			return nil, v2input.ErrInvalid
		}
		remapper.Binary = installed
	}
	resolver, err := NewPreparedImportRecipeResolver(c.Workflow, parsed, reader, sources, remapper, qualifier, publisher)
	if err != nil {
		return nil, err
	}
	return resolver.ResolvePreparedImportRecipe(ctx, entry, target)
}

var _ ImportSchedulerRecipeFactory = SQLImportSchedulerRecipeFactory{}

func (r *scopedSQLImportRecipes) PreparedImportRegistry() *ImportPreparedNodeRegistry {
	if r == nil {
		return nil
	}
	if provider, ok := r.authority.(interface {
		PreparedImportRegistry() *ImportPreparedNodeRegistry
	}); ok {
		return provider.PreparedImportRegistry()
	}
	return nil
}
