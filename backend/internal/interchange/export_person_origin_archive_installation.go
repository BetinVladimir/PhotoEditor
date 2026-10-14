package interchange

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"io"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type ExportPersonOriginArchiveFactory interface {
	BuildExportPersonOriginArchiveAuthority(context.Context, SQLExportOutputCatalog, v2input.Ref,
		ImportDocumentObjects) (*ExportPersonOriginArchiveAuthority, error)
}

// SQLExportPersonOriginArchiveInstallation reconstructs expected receipts from
// sealed task inputs after a restart. Neither ZIP members nor their manifest
// choose the issuer keys or the expected occurrence set.
type SQLExportPersonOriginArchiveInstallation struct {
	Trusted map[PortablePersonOriginKey]ed25519.PublicKey
}

type exportPersonOriginStoredInput struct {
	ref    v2input.Ref
	object objectstore.Object
}

func (s SQLExportPersonOriginArchiveInstallation) BuildExportPersonOriginArchiveAuthority(ctx context.Context,
	config SQLExportOutputCatalog, root v2input.Ref, objects ImportDocumentObjects,
) (*ExportPersonOriginArchiveAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if config.DB == nil || objects == nil || len(s.Trusted) == 0 || !validImportRef(root) ||
		config.Request.Format != "photoeditor_bundle" {
		return nil, v2input.ErrInvalid
	}
	intent, err := FreezeCreateIntent(config.Tenant, config.Who, config.Request, config.Key)
	if err != nil {
		return nil, err
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, intent, config.Frozen.Body())
	if err != nil {
		return nil, err
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil ||
		plan.Direction != "export" || plan.Format != "photoeditor_bundle" {
		return nil, v2input.ErrInvalid
	}
	selected := make(map[uuid.UUID]importPlanEntry, len(plan.Entries))
	for _, entry := range plan.Entries {
		if _, present := selected[entry.ID]; present {
			return nil, v2input.ErrInvalid
		}
		selected[entry.ID] = entry
	}
	stored := make([]exportPersonOriginStoredInput, 0)
	err = tenant.WithTenant(ctx, config.DB, config.Who, config.Tenant, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			rows, err := tx.Query(ctx, `SELECT artifact_id,sha256,bucket,object_key,object_version,bytes,media_type
 FROM photo.v30_export_person_origin_result_inputs($1,$2,$3,$4,$5,$6,$7)`,
				config.Tenant, config.Who.UserID, config.Who.SessionID, config.Key,
				intent.RequestSHA, root.ArtifactID, root.SHA256)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				if len(stored) >= 5000 {
					return v2input.ErrInvalid
				}
				var input exportPersonOriginStoredInput
				var bucket string
				if err := rows.Scan(&input.ref.ArtifactID, &input.ref.SHA256, &bucket,
					&input.object.Key, &input.object.VersionID, &input.object.Bytes,
					&input.object.ContentType); err != nil {
					return err
				}
				input.object.SHA256 = input.ref.SHA256
				if bucket != config.Bucket || !validImportRef(input.ref) ||
					input.object.Key == "" || input.object.VersionID == "" || input.object.VersionID == "null" ||
					input.object.Bytes < 2 || input.object.Bytes > 16384 ||
					input.object.ContentType != "application/json" {
					return v2input.ErrInvalid
				}
				stored = append(stored, input)
			}
			return rows.Err()
		})
	if err != nil {
		return nil, err
	}
	current := SQLExportPersonOriginCurrentChecker{Locator: SQLExportDocumentCatalog{
		DB: config.DB, Tenant: config.Tenant, Who: config.Who, Request: config.Request,
		Key: config.Key, Bucket: config.Bucket, Frozen: config.Frozen}}
	inputs := SQLExportResultInputCatalog{Export: config, Root: root}
	expected := make(map[string]PortablePersonOriginExpectation, len(stored))
	receiptSHA := make(map[string]string, len(stored))
	for _, input := range stored {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		body, err := readExportPersonOriginObject(ctx, objects, input.object, 16384)
		if err != nil || rawSHA(body) != input.ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		var envelope PortablePersonOriginEnvelope
		if json.Unmarshal(body, &envelope) != nil {
			return nil, v2input.ErrInvalid
		}
		claim := envelope.Claim
		scope := PortablePersonOriginExpectation{EntryID: claim.EntryID, Source: claim.Source,
			Recipe: claim.Recipe, Override: claim.Override, MaskRef: claim.MaskRef,
			MaskPixels: claim.MaskPixels, ModelLockSHA: claim.ModelLockSHA}
		verified, err := VerifyPortablePersonOrigin(body, scope, s.Trusted)
		if err != nil || verified != claim ||
			input.ref.ArtifactID != uuid.NewSHA1(scope.EntryID,
				[]byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())) {
			return nil, v2input.ErrInvalid
		}
		entry, present := selected[scope.EntryID]
		if !present || entry.Binding == nil || *entry.Binding != scope.Source ||
			entry.Recipe == nil || *entry.Recipe != scope.Recipe {
			return nil, v2input.ErrInvalid
		}
		maskObject, err := inputs.LookupExportDocument(ctx,
			v2input.Entry{Ref: scope.MaskRef, Contract: "MaskRef"})
		if err != nil || maskObject.ContentType != "application/json" || maskObject.Bytes < 2 ||
			maskObject.Bytes > 1<<20 {
			return nil, v2input.ErrInvalid
		}
		maskBody, err := readExportPersonOriginObject(ctx, objects, maskObject, 1<<20)
		if err != nil || rawSHA(maskBody) != scope.MaskRef.SHA256 {
			return nil, v2input.ErrInvalid
		}
		maskAgain, err := inputs.LookupExportDocument(ctx,
			v2input.Entry{Ref: scope.MaskRef, Contract: "MaskRef"})
		if err != nil || maskAgain != maskObject {
			return nil, v2input.ErrInvalid
		}
		mask, err := review.ParseMaskRef(maskBody)
		if err != nil || mask.Selector != "person" || mask.Protected || mask.ObjectID != nil ||
			mask.Artifact.ID != scope.MaskPixels.ArtifactID || mask.Artifact.SHA256 != scope.MaskPixels.SHA256 ||
			mask.ModelLockID == nil || *mask.ModelLockID != scope.ModelLockSHA {
			return nil, v2input.ErrInvalid
		}
		canvasSHA, err := canonical.Hash(mask.Canvas)
		if err != nil || canvasSHA != scope.Source.CanvasHash {
			return nil, v2input.ErrInvalid
		}
		if err := current.RecheckExportPersonOrigin(ctx, claim); err != nil {
			return nil, err
		}
		path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
		if _, duplicate := expected[path]; duplicate {
			return nil, v2input.ErrInvalid
		}
		expected[path] = scope
		receiptSHA[path] = input.ref.SHA256
	}
	authority, err := NewExportPersonOriginArchiveAuthority(expected, s.Trusted, current)
	if err != nil {
		return nil, err
	}
	if err := authority.pinReceiptSHA(receiptSHA); err != nil {
		return nil, err
	}
	return authority, nil
}

func readExportPersonOriginObject(ctx context.Context, objects ImportDocumentObjects,
	object objectstore.Object, maximum int64) ([]byte, error) {
	stream, err := objects.Open(ctx, object)
	if err != nil {
		return nil, err
	}
	if stream == nil {
		return nil, v2input.ErrInvalid
	}
	body, readErr := io.ReadAll(io.LimitReader(exportContextReader{ctx, stream}, maximum+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if int64(len(body)) != object.Bytes || len(body) < 2 || int64(len(body)) > maximum {
		return nil, v2input.ErrInvalid
	}
	return body, ctx.Err()
}

var _ ExportPersonOriginArchiveFactory = SQLExportPersonOriginArchiveInstallation{}
