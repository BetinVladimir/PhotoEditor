package semanticproposal

import (
	"bytes"
	"context"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func verifyNativeInputSavedTransferParams(raw []byte, expected v2input.SemanticTransferParams) error {
	params, err := v2input.DecodeSemanticTransferParams(raw)
	if err != nil {
		return err
	}
	if params != expected {
		return v2input.ErrConflict
	}
	body, err := canonical.Marshal(expected)
	if err != nil {
		return err
	}
	if !bytes.Equal(raw, body) {
		return v2input.ErrConflict
	}
	return nil
}

// Entire object budget is reserved before payload reads. Every original object
// location is repeated after later IO; no Params documents are recreated.
func (s SQLTransferSourceReader) verifyNativeInputTransferSavedParams(ctx context.Context, who auth.Identity, request Request, published PublishedIntent, saved nativeInputTransferSavedMetadata, configuration TransferPreparation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n := len(saved.params)
	if n < 1 || n > 5000 || n != len(published.Inventory.Targets) || configuration.MetadataBytes <= 0 || configuration.MetadataBytes > 9007199254740991 {
		return v2input.ErrInvalid
	}
	var used int64
	if err := chargeMetadata(&used, configuration.MetadataBytes, n*512); err != nil {
		return err
	}
	objects := make([]objectstore.Object, n)
	for i, ref := range saved.params {
		object, err := s.nativeInputTransferSavedDocumentObject(ctx, who, request, i, ref)
		if err != nil {
			return err
		}
		if err := chargeMetadata(&used, configuration.MetadataBytes, int(object.Bytes)); err != nil {
			return err
		}
		objects[i] = object
	}
	for i, ref := range saved.params {
		raw, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects[i], ref)
		if err != nil {
			return err
		}
		expected := v2input.SemanticTransferParams{SchemaVersion: 1, Intent: published.Ref, Reference: request.Intent.Reference, Target: published.Inventory.Targets[i], ModelLockSHA: configuration.Qualification.ModelLockSHA, Calibration: configuration.Qualification.Calibration}
		if err := verifyNativeInputSavedTransferParams(raw, expected); err != nil {
			return err
		}
	}
	for i, ref := range saved.params {
		after, err := s.nativeInputTransferSavedDocumentObject(ctx, who, request, i, ref)
		if err != nil {
			return err
		}
		if after != objects[i] {
			return v2input.ErrConflict
		}
	}
	return ctx.Err()
}
