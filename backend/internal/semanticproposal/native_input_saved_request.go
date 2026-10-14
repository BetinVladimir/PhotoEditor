package semanticproposal

import (
	"context"
	"reflect"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
)

// Recovers original request identity from an authorized saved detector locator.
// This grants no producer/output/admission authority. Old configs without a key
// still require the original client key; no replacement identity is invented.
func (s SQLTransferSourceReader) restoreNativeInputSavedRequest(ctx context.Context, who auth.Identity, request Request, intent, snapshot v2input.Ref) (Request, string, error) {
	var zero Request
	saved, err := s.readNativeInputSavedStage(ctx, who, request, intent, snapshot, "face.detect")
	if err != nil {
		return zero, "", err
	}
	key := saved.recovery.RequestKey
	if key == "" {
		return zero, "", v2input.ErrConflict
	}
	owned, err := ownNativePlanRequest(ctx, who, request, key)
	if err != nil {
		return zero, "", err
	}
	after, err := s.readNativeInputSavedStage(ctx, who, owned, intent, snapshot, "face.detect")
	if err != nil {
		return zero, "", err
	}
	if !reflect.DeepEqual(saved, after) {
		return zero, "", v2input.ErrConflict
	}
	if err := ctx.Err(); err != nil {
		return zero, "", err
	}
	return owned, key, nil
}
