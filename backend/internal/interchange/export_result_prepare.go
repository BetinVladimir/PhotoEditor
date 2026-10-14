package interchange

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// PreparedExportAdoption is owned pre-commit evidence, never a completed ticket.
// SQL must recheck current canonical result, exact outputs, input inventory and
// authority under locks before persisting links or transitioning workflow state.
type PreparedExportAdoption struct {
	body         []byte
	sha          string
	root, bundle v2input.Ref
}

func (p PreparedExportAdoption) Body() []byte        { return append([]byte(nil), p.body...) }
func (p PreparedExportAdoption) SHA256() string      { return p.sha }
func (p PreparedExportAdoption) Root() v2input.Ref   { return p.root }
func (p PreparedExportAdoption) Bundle() v2input.Ref { return p.bundle }

type ExportResultPreparation struct {
	Catalog             SQLExportOutputCatalog
	Objects             ImportDocumentObjects
	Scratch             *ExportArchiveScratch
	Validator           ExportTypedDocumentValidator
	Binaries            ExportBinaryQualifier
	MaxInputBytes       int64
	PersonOrigins       *ExportPersonOriginArchiveAuthority
	PersonOriginFactory ExportPersonOriginArchiveFactory
}
type exportAdoptionEvidence struct {
	Format        string                  `json:"format"`
	Schema        int                     `json:"schema_version"`
	Tenant        uuid.UUID               `json:"tenant_id"`
	Actor         uuid.UUID               `json:"actor_id"`
	Identity      CreateIdentity          `json:"identity"`
	RequestSHA    string                  `json:"request_sha256"`
	FrozenSHA     string                  `json:"frozen_sha256"`
	Root          v2input.Ref             `json:"root"`
	Documents     []v2input.Ref           `json:"documents"`
	Bundle        v2input.Ref             `json:"bundle"`
	Bucket        string                  `json:"bucket"`
	ObjectKey     string                  `json:"object_key"`
	ObjectVersion string                  `json:"object_version"`
	ArchiveBytes  int64                   `json:"archive_bytes"`
	ManifestSHA   string                  `json:"manifest_sha256"`
	Files         []VerifiedExportZIPFile `json:"files"`
	Sources       []v2input.Entry         `json:"sources"`
}

func (p ExportResultPreparation) Prepare(ctx context.Context, root v2input.Ref) (PreparedExportAdoption, error) {
	var zero PreparedExportAdoption
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if p.Catalog.DB == nil || p.Objects == nil || p.Scratch == nil || p.Validator == nil || p.Binaries == nil || p.MaxInputBytes < 1 || p.MaxInputBytes > 128<<30 {
		return zero, v2input.ErrInvalid
	}
	// Own request/frozen bytes before callbacks and derive the original identity.
	config := p.Catalog
	intent, err := FreezeCreateIntent(config.Tenant, config.Who, config.Request, config.Key)
	if err != nil {
		return zero, err
	}
	if (p.PersonOrigins != nil || p.PersonOriginFactory != nil) && intent.Request.Format != "photoeditor_bundle" {
		return zero, v2input.ErrInvalid
	}
	config.Request = intent.Request
	frozen, _, err := DecodeFrozenExportDocuments(ctx, intent, config.Frozen.Body())
	if err != nil {
		return zero, err
	}
	config.Frozen = frozen
	reader := ExportResultReader{Catalog: config, Objects: p.Objects}
	result, err := reader.Read(ctx, intent, frozen, root)
	if err != nil {
		return zero, err
	}
	if p.PersonOrigins != nil && p.PersonOriginFactory != nil {
		return zero, v2input.ErrInvalid
	}
	origins := p.PersonOrigins
	if p.PersonOriginFactory != nil {
		origins, err = p.PersonOriginFactory.BuildExportPersonOriginArchiveAuthority(ctx, config, root, p.Objects)
		if err != nil {
			return zero, err
		}
		if origins == nil {
			return zero, v2input.ErrInvalid
		}
	}
	archive, err := (ExportArchiveReader{Catalog: config, Objects: p.Objects, Scratch: p.Scratch, PersonOrigins: origins}).Read(ctx, intent, frozen, result.Bundle())
	if err != nil {
		return zero, err
	}
	sources := []v2input.Entry{}
	if intent.Request.Format == "photoeditor_bundle" {
		resolver, err := NewSQLExportResultResolver(ctx, SQLExportResultInputCatalog{Export: config, Root: root}, p.Objects, p.Validator, p.Binaries, p.MaxInputBytes)
		if err != nil {
			return zero, err
		}
		inventory, err := ResolveExportBundleSourceInventory(ctx, intent, frozen, resolver)
		if err != nil {
			return zero, err
		}
		if origins == nil {
			err = ValidateExportBundleSourceInventory(ctx, intent, frozen, archive.Manifest(), inventory)
		} else {
			err = ValidateExportBundleSourceInventoryWithPersonOrigins(ctx, intent, frozen, archive.Manifest(), inventory, origins.expected)
		}
		if err != nil {
			return zero, err
		}
		sources = inventory.Entries()
	}
	// Re-read all result JSON after archive/closure IO to prevent accepting mixed
	// result revisions. Per-artifact lookup also rechecks current canonical scope.
	current, err := reader.Read(ctx, intent, frozen, root)
	if err != nil {
		return zero, err
	}
	if current.Root() != result.Root() || current.Bundle() != result.Bundle() {
		return zero, v2input.ErrInvalid
	}
	oldRefs, newRefs := result.Documents(), current.Documents()
	if len(oldRefs) != len(newRefs) {
		return zero, v2input.ErrInvalid
	}
	for i := range oldRefs {
		if oldRefs[i] != newRefs[i] {
			return zero, v2input.ErrInvalid
		}
	}
	if origins != nil {
		claims := archive.PersonOrigins()
		if len(claims) != len(origins.expected) {
			return zero, v2input.ErrInvalid
		}
		for _, claim := range claims {
			if err := origins.current.RecheckExportPersonOrigin(ctx, claim); err != nil {
				return zero, err
			}
		}
	}
	return freezeExportAdoptionEvidence(ctx, config, intent, frozen, current, archive, sources)
}
func freezeExportAdoptionEvidence(ctx context.Context, config SQLExportOutputCatalog, intent CreateIntent, frozen FrozenExportDocuments, result ValidatedExportResult, archive VerifiedExportArchive, sources []v2input.Entry) (PreparedExportAdoption, error) {
	var zero PreparedExportAdoption
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	checked, err := FreezeCreateIntent(config.Tenant, config.Who, config.Request, config.Key)
	if err != nil || checked.Identity != intent.Identity || checked.RequestSHA != intent.RequestSHA {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); err != nil {
		return zero, err
	}
	if (intent.Request.Format == "photoeditor_bundle" && len(sources) == 0) || (intent.Request.Format != "photoeditor_bundle" && len(sources) != 0) {
		return zero, v2input.ErrInvalid
	}
	if !validImportRef(result.Root()) || result.Bundle() != archive.Bundle() || !validImportRef(archive.Bundle()) || archive.bundle.object.Bytes < 22 || archive.bundle.object.Key == "" || archive.bundle.object.VersionID == "" || archive.bundle.object.VersionID == "null" || archive.bundle.object.SHA256 != archive.Bundle().SHA256 || len(archive.files) == 0 || len(archive.manifest) < 2 || config.Tenant == uuid.Nil || config.Who.UserID == uuid.Nil || len(config.Bucket) < 3 {
		return zero, v2input.ErrInvalid
	}
	evidence := exportAdoptionEvidence{Format: intent.Request.Format, Schema: 1, Tenant: config.Tenant, Actor: config.Who.UserID, Identity: intent.Identity, RequestSHA: intent.RequestSHA, FrozenSHA: rawSHA(frozen.Body()), Root: result.Root(), Documents: result.Documents(), Bundle: archive.Bundle(), Bucket: config.Bucket, ObjectKey: archive.bundle.object.Key, ObjectVersion: archive.bundle.object.VersionID, ArchiveBytes: archive.bundle.object.Bytes, ManifestSHA: rawSHA(archive.manifest), Files: archive.Files(), Sources: append([]v2input.Entry{}, sources...)}
	body, err := canonical.Marshal(evidence)
	if err != nil || len(body) > 16<<20 {
		return zero, v2input.ErrInvalid
	}
	return PreparedExportAdoption{body: body, sha: rawSHA(body), root: result.Root(), bundle: archive.Bundle()}, ctx.Err()
}
