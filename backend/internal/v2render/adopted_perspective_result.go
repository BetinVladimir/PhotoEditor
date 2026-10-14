package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type AdoptedPerspectiveObjectSource interface {
	LoadObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, string) (AdoptedPerspectiveBinding, objectstore.Object, error)
}

// LoadAdoptedPerspectiveResult performs object IO after SQL transactions close. The
// object reader must honor immutable VersionID and context cancellation. Child
// refs remain subject to separate scoped resolution; parsing does not grant IO.
func LoadAdoptedPerspectiveResult(ctx context.Context, source AdoptedPerspectiveObjectSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, expected v2input.Ref, bucket string) (AdoptedPerspectiveBinding, v2draft.ImageResultDocument, error) {
	empty := v2draft.ImageResultDocument{}
	if source == nil || objects == nil {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrInvalid
	}
	binding, obj, err := source.LoadObject(ctx, who, tenant, project, version, producer, expected, bucket)
	if err != nil {
		return AdoptedPerspectiveBinding{}, empty, err
	}
	if err := validateAdoptedPerspectiveBinding(binding, expected, version); err != nil {
		return AdoptedPerspectiveBinding{}, empty, err
	}
	if obj.Bytes < 2 || obj.Bytes > 1048576 || obj.SHA256 != expected.SHA256 || obj.ContentType != "application/json" || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return AdoptedPerspectiveBinding{}, empty, err
	}
	if reader == nil {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrInvalid
	}
	body, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return AdoptedPerspectiveBinding{}, empty, readErr
	}
	if closeErr != nil {
		return AdoptedPerspectiveBinding{}, empty, closeErr
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != expected.SHA256 {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrInvalid
	}
	jcs, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(jcs, body) {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrInvalid
	}
	document, err := v2draft.ParseImageResult(body, expected)
	if err != nil {
		return AdoptedPerspectiveBinding{}, empty, err
	}
	if err := ctx.Err(); err != nil {
		return AdoptedPerspectiveBinding{}, empty, err
	}
	current, currentObject, err := source.LoadObject(ctx, who, tenant, project, version, producer, expected, bucket)
	if err != nil {
		return AdoptedPerspectiveBinding{}, empty, err
	}
	if current != binding || currentObject.Key != obj.Key || currentObject.VersionID != obj.VersionID || currentObject.SHA256 != obj.SHA256 || currentObject.Bytes != obj.Bytes || currentObject.ContentType != obj.ContentType {
		return AdoptedPerspectiveBinding{}, empty, v2input.ErrExpired
	}
	return binding, document, nil
}
