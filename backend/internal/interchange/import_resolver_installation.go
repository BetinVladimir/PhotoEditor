package interchange

import (
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/v2input"
)

type ImportResolverDependencies struct {
	Prepared       *ImportPreparedNodeRegistry
	Reviews        *ImportReviewWitnesses
	DB             *pgxpool.Pool
	Objects        ImportDocumentObjects
	Bucket         string
	MaxFileBytes   int64
	WarpScratch    *ExportArchiveScratch
	WarpProbe      ExportNativeProbe
	WarpRuntimeSHA string
}

// NewSQLImportResolver shares the verified master witnesses between typed JSON
// traversal and canonical pixel lookup. Recipe dependency installation remains
// explicit; unavailable typed contracts fail closed instead of losing refs.
func NewSQLImportResolver(tenant, workflow, project uuid.UUID, snapshot string, planRaw, parsedRaw []byte, deps ImportResolverDependencies) (ImportNodeResolverRouter, error) {
	if tenant == uuid.Nil || workflow == uuid.Nil || deps.DB == nil || deps.Objects == nil || len(deps.Bucket) < 3 || len(deps.Bucket) > 63 || deps.MaxFileBytes < 1 || deps.MaxFileBytes > 128<<30 {
		return ImportNodeResolverRouter{}, v2input.ErrInvalid
	}
	validator, err := NewImportScopedDocumentValidator(project, snapshot, planRaw, parsedRaw)
	if err != nil {
		return ImportNodeResolverRouter{}, err
	}
	parsed := append([]byte(nil), parsedRaw...)
	jsonCatalog := SQLImportDocumentCatalog{DB: deps.DB, Tenant: tenant, Workflow: workflow, Parsed: parsed, Bucket: deps.Bucket}
	opaqueCatalog := SQLImportOpaqueCatalog{DB: deps.DB, Tenant: tenant, Workflow: workflow, Parsed: parsed, Bucket: deps.Bucket}
	pixelCatalog := SQLImportPixelCatalog{DB: deps.DB, Tenant: tenant, Workflow: workflow, Parsed: parsed, Bucket: deps.Bucket, Witnesses: validator.WorkingWitnesses()}
	warpCatalog := SQLImportWarpCatalog{DB: deps.DB, Tenant: tenant, Workflow: workflow, Parsed: parsed, Bucket: deps.Bucket, Witnesses: validator.WorkingWitnesses()}
	var warpQualifier ExportBinaryQualifier
	if deps.WarpScratch != nil || deps.WarpProbe != nil || deps.WarpRuntimeSHA != "" {
		if deps.WarpScratch == nil || deps.WarpProbe == nil || !validImportRefForSHA(deps.WarpRuntimeSHA) {
			return ImportNodeResolverRouter{}, v2input.ErrInvalid
		}
		warpQualifier = NativeExportArrayQualifier{Objects: deps.Objects, Scratch: deps.WarpScratch, Probe: deps.WarpProbe, RuntimeSHA: deps.WarpRuntimeSHA, Geometry: ImportCanonicalWarpGeometry{Witnesses: validator.WorkingWitnesses(), Catalog: warpCatalog}}
	}
	return ImportNodeResolverRouter{
		Prepared: deps.Prepared,
		JSON:     ImportJSONNodeResolver{Catalog: jsonCatalog, Objects: deps.Objects, Validator: validator},
		Opaque:   ImportOpaqueNodeResolver{Catalog: opaqueCatalog, Objects: deps.Objects, MaxBytes: deps.MaxFileBytes},
		Warps:    ImportCanonicalWarpNodeResolver{Catalog: warpCatalog, Objects: deps.Objects, MaxBytes: deps.MaxFileBytes, Qualifier: warpQualifier},
		Pixels:   ImportCanonicalPixelNodeResolver{Catalog: pixelCatalog, Objects: deps.Objects, MaxBytes: deps.MaxFileBytes},
	}, nil
}
