package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// PerspectiveSourceArrayCatalog must resolve accepted source/child authority in
// a short transaction and commit before returning an immutable object version.
// The caller must repeat source locks inside the eventual admission transaction.
type PerspectiveSourceArrayCatalog interface {
	ResolvePerspectiveSourceArray(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PerspectiveParams, []byte, PerspectiveSourceArraySpec) (objectstore.Object, error)
}

// VerifyPerspectiveSourceObjects streams each occurrence, including shared dense
// fields, and rechecks its live locator. Success is not an admission capability.
func VerifyPerspectiveSourceObjects(ctx context.Context, catalog PerspectiveSourceArrayCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, snapshot uuid.UUID, params PerspectiveParams, sourceRaw []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil {
		return v2input.ErrInvalid
	}
	body, _, err := params.Compile()
	if err != nil {
		return err
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	params, err = ParsePerspectiveParams(body)
	if err != nil {
		return err
	}
	specs, err := PerspectiveSourceArrays(params, sourceRaw)
	if err != nil {
		return err
	}
	// Each callback gets fresh owned bytes/slices: it cannot alter later checks.
	resolve := func(spec PerspectiveSourceArraySpec) (objectstore.Object, error) {
		if err := ctx.Err(); err != nil {
			return objectstore.Object{}, err
		}
		owned, err := ParsePerspectiveParams(body)
		if err != nil {
			return objectstore.Object{}, err
		}
		return catalog.ResolvePerspectiveSourceArray(ctx, who, tenant, project, snapshot, owned, append([]byte(nil), sourceRaw...), spec)
	}
	pinned := make([]objectstore.Object, len(specs))
	for i, spec := range specs {
		object, err := resolve(spec)
		if err != nil {
			return err
		}
		reader, err := objects.Open(ctx, object)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return err
		}
		if reader == nil {
			return v2input.ErrInvalid
		}
		verifyErr := VerifyPerspectiveSourceArray(ctx, spec, object, reader)
		closeErr := reader.Close()
		if verifyErr != nil {
			return verifyErr
		}
		if closeErr != nil {
			return closeErr
		}
		current, err := resolve(spec)
		if err != nil {
			return err
		}
		if current != object {
			return v2input.ErrExpired
		}
		pinned[i] = object
	}
	for i, spec := range specs {
		current, err := resolve(spec)
		if err != nil {
			return err
		}
		if current != pinned[i] {
			return v2input.ErrExpired
		}
	}
	return ctx.Err()
}
