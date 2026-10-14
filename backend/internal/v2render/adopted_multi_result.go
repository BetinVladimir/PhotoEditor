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

type AdoptedMultiObjectSource interface {
	LoadObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, v2input.Ref, string) (AdoptedMultiBinding, objectstore.Object, error)
}

// Read one immutable adopted root outside SQL transactions. This is a root
// proof only: callers must independently resolve its exact typed child closure.
func LoadAdoptedMultiResult(ctx context.Context, source AdoptedMultiObjectSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, operation string, expected v2input.Ref, bucket string) (AdoptedMultiBinding, v2draft.ImageResultDocument, error) {
	empty := v2draft.ImageResultDocument{}
	if source == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || producer == uuid.Nil || bucket == "" {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	if err := ctx.Err(); err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	b, obj, err := source.LoadObject(ctx, who, tenant, project, producer, operation, expected, bucket)
	if err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if err = validateAdoptedMultiBinding(b, producer, operation, expected); err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if b.Replacement!=(MultiReplacementBindingIdentity{})&&(b.Replacement.Tenant!=tenant||b.Replacement.Project!=project){return AdoptedMultiBinding{},empty,v2input.ErrInvalid}
	if !validAdoptedMultiRootObject(obj, expected) {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		if reader != nil {
			_ = reader.Close()
		}
		return AdoptedMultiBinding{}, empty, err
	}
	if reader == nil {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	body, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
	closeErr := reader.Close()
	if readErr != nil {
		return AdoptedMultiBinding{}, empty, readErr
	}
	if closeErr != nil {
		return AdoptedMultiBinding{}, empty, closeErr
	}
	digest := sha256.Sum256(body)
	if hex.EncodeToString(digest[:]) != expected.SHA256 {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	jcs, err := canonical.Bytes(body)
	if err != nil || !bytes.Equal(jcs, body) {
		return AdoptedMultiBinding{}, empty, v2input.ErrInvalid
	}
	document, err := v2draft.ParseImageResult(body, expected)
	if err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if err = validateMultiResultRoot(b, document); err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if err = ctx.Err(); err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	current, currentObject, err := source.LoadObject(ctx, who, tenant, project, producer, operation, expected, bucket)
	if err != nil {
		return AdoptedMultiBinding{}, empty, err
	}
	if current != b || currentObject != obj {
		return AdoptedMultiBinding{}, empty, v2input.ErrExpired
	}
	return b, document, nil
}
func validateMultiResultRoot(binding AdoptedMultiBinding, document v2draft.ImageResultDocument) error {
	if document.GeneratedMask != nil {
		return v2input.ErrInvalid
	}
	frames, _, calibration, err := binding.Inputs()
	if err != nil {
		return err
	}
	used := map[uuid.UUID]bool{binding.Result.ArtifactID: true, calibration.ArtifactID: true}
	for _, frame := range frames {
		used[frame.Source.ArtifactID] = true
	}
	for _, ref := range []v2input.Ref{document.WorkingImage, document.Report, document.ChangedMask} {
		if !validLayerArtifactRef(ref) || used[ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		used[ref.ArtifactID] = true
	}
	return nil
}
