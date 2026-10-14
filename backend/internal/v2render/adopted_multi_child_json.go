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
	"photoeditor/backend/internal/v2input"
)

type AdoptedMultiChildSource interface {
	AdoptedMultiObjectSource
	LoadChildObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, v2input.Ref, v2input.Ref, string, string, string) (objectstore.Object, error)
}

// Byte verification only. The caller must parse the named contract and verify
// its ordered source/geometry references before using this document in review.
func LoadAdoptedMultiJSONChild(ctx context.Context, source AdoptedMultiChildSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, binding AdoptedMultiBinding, child v2input.Ref, path, contract, bucket string) ([]byte, error) {
	media, limit, allowed := multiChildObjectLimit(path, contract)
	if source == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || bucket == "" || !allowed || media != "application/json" || !validLayerArtifactRef(child) || child.ArtifactID == binding.Result.ArtifactID {
		return nil, v2input.ErrInvalid
	}
	if err := validateAdoptedMultiBinding(binding, producer, binding.Operation, binding.Result); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	current, rootObject, err := source.LoadObject(ctx, who, tenant, project, producer, binding.Operation, binding.Result, bucket)
	if err != nil {
		return nil, err
	}
	if current != binding {
		return nil, v2input.ErrExpired
	}
	if !validAdoptedMultiRootObject(rootObject, binding.Result) {
		return nil, v2input.ErrInvalid
	}
	obj, err := source.LoadChildObject(ctx, who, tenant, project, producer, binding.Operation, binding.Result, child, path, contract, bucket)
	if err != nil {
		return nil, err
	}
	if !validMultiChildObject(obj, child, media, limit, false) {
		return nil, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return nil, err
	}
	if reader == nil {
		return nil, v2input.ErrInvalid
	}
	body, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != child.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(normalized, body) {
		return nil, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	next, err := source.LoadChildObject(ctx, who, tenant, project, producer, binding.Operation, binding.Result, child, path, contract, bucket)
	if err != nil {
		return nil, err
	}
	if next != obj {
		return nil, v2input.ErrExpired
	}
	current, nextRoot, err := source.LoadObject(ctx, who, tenant, project, producer, binding.Operation, binding.Result, bucket)
	if err != nil {
		return nil, err
	}
	if current != binding || nextRoot != rootObject {
		return nil, v2input.ErrExpired
	}
	return bytes.Clone(body), nil
}
