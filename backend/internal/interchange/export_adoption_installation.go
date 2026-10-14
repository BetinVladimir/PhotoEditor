package interchange

import (
	"context"
	"crypto/ed25519"

	"photoeditor/backend/internal/v2input"
)

// SQLExportResultInstaller binds physical/object and issuer authority at
// process startup. Each request gets a fresh SQL-scoped output catalog.
type SQLExportResultInstaller struct {
	Objects             ImportDocumentObjects
	Scratch             *ExportArchiveScratch
	Validator           ExportTypedDocumentValidator
	ValidatorFactory    ExportDocumentValidatorFactory
	Binaries            ExportBinaryQualifier
	Native              *ExportNativeResolverInstallation
	MaxInputBytes       int64
	PersonOriginTrusted map[PortablePersonOriginKey]ed25519.PublicKey
}

func (s SQLExportResultInstaller) InstallExportResultPreparation(ctx context.Context,
	config SQLExportOutputCatalog, root v2input.Ref,
) (ExportResultPreparation, error) {
	var zero ExportResultPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if config.DB == nil || s.Objects == nil || s.Scratch == nil ||
		(s.Validator == nil) == (s.ValidatorFactory == nil) || !validImportRef(root) ||
		(s.Binaries == nil) == (s.Native == nil) || s.MaxInputBytes < 1 ||
		s.MaxInputBytes > 128<<30 {
		return zero, v2input.ErrInvalid
	}
	if s.Native != nil && (s.Native.Scratch != s.Scratch || s.Native.Catalog != nil || s.Native.Objects != nil || s.Native.JSON != nil || s.Native.MaxBytes != 0 || s.Native.ImageProbe == nil || s.Native.MaskProbe == nil || s.Native.ArrayProbe == nil || !validImportRef(v2input.Ref{ArtifactID: root.ArtifactID, SHA256: s.Native.ImageRuntimeSHA}) || !validImportRef(v2input.Ref{ArtifactID: root.ArtifactID, SHA256: s.Native.ArrayRuntimeSHA})) {
		return zero, v2input.ErrInvalid
	}
	intent, err := FreezeCreateIntent(config.Tenant, config.Who, config.Request, config.Key)
	if err != nil {
		return zero, err
	}
	frozen, _, err := DecodeFrozenExportDocuments(ctx, intent, config.Frozen.Body())
	if err != nil {
		return zero, err
	}
	validator := s.Validator
	if s.ValidatorFactory != nil {
		catalog := SQLExportResultInputCatalog{Export: config, Root: root}
		if scoped, ok := s.ValidatorFactory.(ExportScopedDocumentValidatorFactory); ok {
			validator, err = scoped.BuildScopedExportDocumentValidator(ctx, intent, frozen, catalog, s.Objects)
		} else {
			validator, err = s.ValidatorFactory.BuildExportDocumentValidator(ctx, intent, frozen)
		}
		if err != nil {
			return zero, err
		}
		if validator == nil {
			return zero, v2input.ErrInvalid
		}
		validator, err = NewExportScopedDocumentValidator(ctx, intent, frozen, validator)
		if err != nil {
			return zero, err
		}
	}
	var native *ExportNativeResolverInstallation
	if s.Native != nil {
		owned := *s.Native
		native = &owned
	}
	preparation := ExportResultPreparation{Catalog: config, Objects: s.Objects,
		Scratch: s.Scratch, Validator: validator, Binaries: s.Binaries, Native: native,
		MaxInputBytes: s.MaxInputBytes}
	if intent.Request.Format == "photoeditor_bundle" {
		if len(s.PersonOriginTrusted) == 0 {
			return zero, v2input.ErrInvalid
		}
		trusted := make(map[PortablePersonOriginKey]ed25519.PublicKey, len(s.PersonOriginTrusted))
		for key, public := range s.PersonOriginTrusted {
			trusted[key] = append(ed25519.PublicKey(nil), public...)
		}
		if _, err := NewExportPersonOriginArchiveAuthority(
			map[string]PortablePersonOriginExpectation{}, trusted, nil); err != nil {
			return zero, err
		}
		preparation.PersonOriginFactory = SQLExportPersonOriginArchiveInstallation{Trusted: trusted}
	}
	return preparation, nil
}

var _ ExportResultPreparationInstaller = SQLExportResultInstaller{}
