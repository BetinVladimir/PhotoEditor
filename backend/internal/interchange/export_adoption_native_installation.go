package interchange

import (
	"bytes"
	"context"
	"crypto/ed25519"

	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// SchedulerExportNativeInstallation owns the same pinned probes and scoped
// document validator as foreground export. Each claimed result gets a fresh
// graph resolver; no user session or request-selected validator is accepted.
type SchedulerExportNativeInstallation struct {
	Installer SQLExportResultInstaller
}

func (s SchedulerExportNativeInstallation) BuildSchedulerExportNativeResolver(ctx context.Context,
	claim ExportAdoptionClaim, result SchedulerExportResult,
	catalog SQLSchedulerExportInputCatalog, objects ImportDocumentObjects,
) (ExportClosureNodeResolver, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if claim.validate() != nil || catalog.Claim.validate() != nil ||
		objects == nil || catalog.Claims.DB == nil ||
		catalog.Claim.Tenant != claim.Tenant || catalog.Claim.Workflow != claim.Workflow ||
		catalog.Claim.Run != claim.Run || catalog.Claim.Task != claim.Task ||
		catalog.Claim.Result != claim.Result || catalog.Claim.Actor != claim.Actor ||
		catalog.Claim.Token != claim.Token || catalog.Claim.Key != claim.Key ||
		catalog.Claim.RequestSHA != claim.RequestSHA || catalog.Bucket == "" ||
		s.Installer.Native == nil || s.Installer.Scratch == nil ||
		s.Installer.Native.Scratch != s.Installer.Scratch ||
		s.Installer.Objects == nil || s.Installer.MaxInputBytes < 1 ||
		s.Installer.MaxInputBytes > 128<<30 {
		return nil, v2input.ErrInvalid
	}
	validatorFactory, ok := s.Installer.ValidatorFactory.(ExportScopedDocumentValidatorFactory)
	if !ok || s.Installer.Validator != nil || s.Installer.Binaries != nil ||
		result.Intent.Identity.Task != claim.Task ||
		result.Intent.RequestSHA != claim.RequestSHA ||
		result.Intent.Request.Format != "photoeditor_bundle" {
		return nil, v2input.ErrInvalid
	}
	wanted, err := exportIntentFromClaim(claim)
	if err != nil || result.Intent.Identity != wanted.Identity ||
		!bytes.Equal(result.Intent.RequestJCS, wanted.RequestJCS) {
		return nil, v2input.ErrInvalid
	}
	if _, _, err := DecodeFrozenExportDocuments(ctx, wanted, result.Frozen.Body()); err != nil {
		return nil, err
	}
	var background ExportStyleBackgroundAuthority
	if style, ok := validatorFactory.(ExportStyleValidatorInstallation); ok {
		background, err = style.BuildScopedExportBackground(ctx, wanted, result.Frozen, catalog, objects)
		if err != nil {
			return nil, err
		}
		style.Backgrounds = background
		style.BackgroundFactory = nil
		validatorFactory = style
	}
	validator, err := validatorFactory.BuildScopedExportDocumentValidator(ctx,
		wanted, result.Frozen, catalog, objects)
	if err != nil {
		return nil, err
	}
	if validator == nil {
		return nil, v2input.ErrInvalid
	}
	validator, err = NewExportScopedDocumentValidator(ctx, wanted, result.Frozen, validator)
	if err != nil {
		return nil, err
	}
	native := *s.Installer.Native
	if background != nil {
		if native.Backgrounds != nil {
			return nil, v2input.ErrInvalid
		}
		native.Backgrounds = background
	}
	native.Catalog = catalog
	native.Objects = objects
	native.JSON = ExportJSONNodeResolver{Catalog: catalog, Objects: objects,
		Validator: validator}
	native.MaxBytes = s.Installer.MaxInputBytes
	if native.ImageProbe == nil || native.MaskProbe == nil || native.ArrayProbe == nil ||
		!validImportRefForSHA(native.ImageRuntimeSHA) ||
		!validImportRefForSHA(native.ArrayRuntimeSHA) {
		return nil, v2input.ErrInvalid
	}
	return native, ctx.Err()
}

// BuildSchedulerExportAdoptionFromDeployment wires physical archive and native
// closure reads from one exact deployment, then checks scheduler SQL authority.
func (d NativeExportDeployment) BuildSchedulerExportAdoptionFromDeployment(ctx context.Context,
	db *pgxpool.Pool, objects *objectstore.Store, bucket string,
	authority ExportNativeAuthorityFactory,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (SchedulerExportAdoptionProcessor, error) {
	var zero SchedulerExportAdoptionProcessor
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if db == nil || objects == nil || authority == nil || len(trusted) == 0 {
		return zero, v2input.ErrInvalid
	}
	installer, err := d.BuildResultInstallerWithAuthority(ctx, objects, authority, trusted)
	if err != nil {
		return zero, err
	}
	return BuildSchedulerExportAdoptionProcessor(ctx, db, objects, bucket,
		installer.Scratch, trusted, SchedulerExportNativeInstallation{Installer: installer})
}

var _ SchedulerExportNativeResolverFactory = SchedulerExportNativeInstallation{}
