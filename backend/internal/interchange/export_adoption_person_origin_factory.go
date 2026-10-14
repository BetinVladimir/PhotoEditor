package interchange

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// SQLSchedulerExportPersonOriginInstallation reconstructs the expected ZIP
// receipts from sealed task inputs, process-provisioned trust and the frozen
// export plan. It never borrows a user's expired HTTP session.
type SQLSchedulerExportPersonOriginInstallation struct {
	Claims  SQLExportAdoptionClaimStore
	Bucket  string
	Trusted map[PortablePersonOriginKey]ed25519.PublicKey
}

func (s SQLSchedulerExportPersonOriginInstallation) BuildSchedulerExportPersonOrigins(ctx context.Context,
	claim ExportAdoptionClaim, result SchedulerExportResult, objects ImportDocumentObjects,
) (*ExportPersonOriginArchiveAuthority, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Claims.DB == nil || objects == nil || len(s.Trusted) == 0 || claim.validate() != nil ||
		claim.Request.Format != "photoeditor_bundle" || result.Intent.RequestSHA != claim.RequestSHA ||
		result.Result.Root().ArtifactID == uuid.Nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	wanted, err := exportIntentFromClaim(claim)
	if err != nil || result.Intent.Identity != wanted.Identity ||
		!bytes.Equal(result.Intent.RequestJCS, wanted.RequestJCS) {
		return nil, v2input.ErrInvalid
	}
	_, compiled, err := DecodeFrozenExportDocuments(ctx, result.Intent, result.Frozen.Body())
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
	inputReader := SchedulerExportPersonOriginInputs{Claims: s.Claims, Bucket: s.Bucket}
	stored, err := inputReader.Read(ctx, claim)
	if err != nil {
		return nil, err
	}
	current := SchedulerExportPersonOriginCurrentChecker{Claims: s.Claims, Claim: claim}
	catalog := SQLSchedulerExportInputCatalog{Claims: s.Claims, Claim: claim, Bucket: s.Bucket}
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
		origin := envelope.Claim
		scope := PortablePersonOriginExpectation{EntryID: origin.EntryID, Source: origin.Source,
			Recipe: origin.Recipe, Override: origin.Override, MaskRef: origin.MaskRef,
			MaskPixels: origin.MaskPixels, ModelLockSHA: origin.ModelLockSHA}
		verified, err := VerifyPortablePersonOrigin(body, scope, s.Trusted)
		if err != nil || verified != origin || input.ref.ArtifactID != uuid.NewSHA1(scope.EntryID,
			[]byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())) {
			return nil, v2input.ErrInvalid
		}
		entry, present := selected[scope.EntryID]
		if !present || entry.Binding == nil || *entry.Binding != scope.Source ||
			entry.Recipe == nil || *entry.Recipe != scope.Recipe {
			return nil, v2input.ErrInvalid
		}
		maskObject, err := catalog.LookupExportDocument(ctx,
			v2input.Entry{Ref: scope.MaskRef, Contract: "MaskRef"})
		if err != nil || maskObject.ContentType != "application/json" || maskObject.Bytes < 2 ||
			maskObject.Bytes > 1<<20 {
			return nil, v2input.ErrInvalid
		}
		maskBody, err := readExportPersonOriginObject(ctx, objects, maskObject, 1<<20)
		if err != nil || rawSHA(maskBody) != scope.MaskRef.SHA256 {
			return nil, v2input.ErrInvalid
		}
		maskAgain, err := catalog.LookupExportDocument(ctx,
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
		if err := current.RecheckExportPersonOrigin(ctx, origin); err != nil {
			return nil, err
		}
		path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
		if _, duplicate := expected[path]; duplicate {
			return nil, v2input.ErrInvalid
		}
		expected[path] = scope
		receiptSHA[path] = input.ref.SHA256
	}
	latest, err := inputReader.Read(ctx, claim)
	if err != nil {
		return nil, err
	}
	if !sameSchedulerPersonOriginInputs(stored, latest) {
		return nil, v2input.ErrInvalid
	}
	authority, err := NewExportPersonOriginArchiveAuthority(expected, s.Trusted, current)
	if err != nil {
		return nil, err
	}
	if err := authority.pinReceiptSHA(receiptSHA); err != nil {
		return nil, err
	}
	return authority, ctx.Err()
}

func sameSchedulerPersonOriginInputs(left, right []exportPersonOriginStoredInput) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

var _ SchedulerExportPersonOriginFactory = SQLSchedulerExportPersonOriginInstallation{}
