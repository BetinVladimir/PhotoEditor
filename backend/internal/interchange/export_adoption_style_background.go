package interchange

import (
	"bytes"
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func (s SQLSchedulerExportInputCatalog) LookupExportStyleBackground(ctx context.Context,
	pin v2draft.FrozenBackgroundAsset) (objectstore.Object, error) {
	var zero objectstore.Object
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.Claims.DB == nil || s.Claim.validate() != nil || s.Bucket == "" {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(pin)
	if err != nil {
		return zero, err
	}
	if _, err = v2draft.DecodeFrozenBackgroundAsset(raw); err != nil {
		return zero, err
	}
	style, err := s.LookupExportDocument(ctx, v2input.Entry{Ref: pin.Style, Contract: "StyleV2"})
	if err != nil {
		return zero, err
	}
	source, err := s.LookupExportDocument(ctx, v2input.Entry{Ref: pin.Source, Contract: "StyleBackgroundAsset"})
	if err != nil {
		return zero, err
	}
	var bucket string
	var object objectstore.Object
	err = s.Claims.DB.QueryRow(ctx, `SELECT bucket,object_key,object_version,sha256,bytes,media_type
 FROM photo.v30_export_adoption_style_background(
 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
		s.Claim.Tenant, s.Claim.Workflow, s.Claim.Run, s.Claim.Task,
		s.Claim.Result, s.Claim.Actor, s.Claim.Key, s.Claim.RequestSHA,
		s.Claim.Token, pin.StyleVersionID, pin.Style.ArtifactID,
		pin.Style.SHA256, pin.PortableAssetID, pin.Source.ArtifactID,
		pin.Source.SHA256, pin.ExpectedBytes, pin.MediaType, s.Bucket).
		Scan(&bucket, &object.Key, &object.VersionID, &object.SHA256,
			&object.Bytes, &object.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrInvalid
	}
	if err != nil {
		return zero, err
	}
	if bucket != s.Bucket || object != source || style.ContentType != "application/json" ||
		object.SHA256 != pin.Source.SHA256 || object.Bytes != pin.ExpectedBytes ||
		object.ContentType != pin.MediaType {
		return zero, v2input.ErrInvalid
	}
	styleAgain, err := s.LookupExportDocument(ctx, v2input.Entry{Ref: pin.Style, Contract: "StyleV2"})
	if err != nil {
		return zero, err
	}
	sourceAgain, err := s.LookupExportDocument(ctx, v2input.Entry{Ref: pin.Source, Contract: "StyleBackgroundAsset"})
	if err != nil {
		return zero, err
	}
	if styleAgain != style || sourceAgain != source {
		return zero, v2input.ErrInvalid
	}
	return object, ctx.Err()
}

// SQLSchedulerExportStyleBackgroundAuthority repeats the current binding after
// physical native qualification; an expired claim cannot authorize a return.
type SQLSchedulerExportStyleBackgroundAuthority struct {
	Catalog SQLSchedulerExportInputCatalog
	Images  NativeExportImageQualifier
}

func (s SQLSchedulerExportStyleBackgroundAuthority) QualifyExportStyleBackground(ctx context.Context,
	pin v2draft.FrozenBackgroundAsset) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.Images.Objects == nil || s.Images.Scratch == nil || s.Images.Probe == nil {
		return v2input.ErrInvalid
	}
	object, err := s.Catalog.LookupExportStyleBackground(ctx, pin)
	if err != nil {
		return err
	}
	if err := s.Images.QualifyExportBinary(ctx,
		v2input.Entry{Ref: pin.Source, Contract: "EncodedImage"}, object); err != nil {
		return err
	}
	current, err := s.Catalog.LookupExportStyleBackground(ctx, pin)
	if err != nil {
		return err
	}
	if current != object {
		return v2input.ErrInvalid
	}
	return ctx.Err()
}

// The same operator manifest can install both API and scheduler scopes, while
// each request/claim receives a fresh authority with its own SQL fence.
type SQLExportStyleBackgroundFactory struct{}

func (SQLExportStyleBackgroundFactory) BuildExportStyleBackgroundAuthority(ctx context.Context,
	intent CreateIntent, frozen FrozenExportDocuments, catalog ExportDocumentCatalog,
	objects ImportDocumentObjects, images NativeExportImageQualifier,
) (ExportStyleBackgroundAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if scheduler, ok := catalog.(SQLSchedulerExportInputCatalog); ok {
		if scheduler.Claims.DB == nil || scheduler.Claim.validate() != nil ||
			objects == nil || images.Objects == nil || images.Scratch == nil || images.Probe == nil ||
			!validImportRefForSHA(images.RuntimeSHA) {
			return nil, v2input.ErrInvalid
		}
		wanted, err := exportIntentFromClaim(scheduler.Claim)
		if err != nil || wanted.Identity != intent.Identity || wanted.RequestSHA != intent.RequestSHA ||
			!bytes.Equal(wanted.RequestJCS, intent.RequestJCS) {
			return nil, v2input.ErrInvalid
		}
		if _, _, err := DecodeFrozenExportDocuments(ctx, intent, frozen.Body()); err != nil {
			return nil, err
		}
		return SQLSchedulerExportStyleBackgroundAuthority{Catalog: scheduler, Images: images}, ctx.Err()
	}
	return (SQLAPIExportStyleBackgroundFactory{}).BuildExportStyleBackgroundAuthority(
		ctx, intent, frozen, catalog, objects, images)
}

var _ ExportStyleBackgroundAuthority = SQLSchedulerExportStyleBackgroundAuthority{}
var _ ExportStyleBackgroundAuthorityFactory = SQLExportStyleBackgroundFactory{}
