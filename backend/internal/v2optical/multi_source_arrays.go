package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type MultiSourceArraySpec struct {
	Ordinal int
	Binding v2input.SessionVersionBinding
	Array   PerspectiveSourceArraySpec
}

func MultiSourceArrays(operation string, paramsRaw []byte, originals [][]byte) ([]MultiSourceArraySpec, error) {
	images, err := VerifyMultiWorkingImages(operation, paramsRaw, originals)
	if err != nil {
		return nil, err
	}
	var frames []v2input.SessionVersionBinding
	if operation == "optical.panorama" {
		p, _ := ParsePanoramaParams(paramsRaw)
		frames = p.Frames
	} else {
		p, _ := ParseFocusStackParams(paramsRaw)
		frames = p.Frames
	}
	var all []MultiSourceArraySpec
	for ordinal, image := range images {
		specs, err := sourceImageArraySpecs(image)
		if err != nil {
			return nil, err
		}
		for _, spec := range specs {
			all = append(all, MultiSourceArraySpec{Ordinal: ordinal, Binding: frames[ordinal], Array: spec})
		}
	}
	return all, nil
}

// Resolve must prove current source/child scope in a short committed transaction.
// No object stream is opened while holding that transaction.
type MultiSourceArrayCatalog interface {
	ResolveMultiSourceArray(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, string, []byte, []byte, MultiSourceArraySpec) (objectstore.Object, error)
}

func VerifyMultiSourceObjects(ctx context.Context, catalog MultiSourceArrayCatalog, objects v2input.SessionAnalysisObjectReader,
	who auth.Identity, tenant, project, snapshot uuid.UUID, operation string, paramsRaw []byte, originals [][]byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if catalog == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil {
		return v2input.ErrInvalid
	}
	body := append([]byte(nil), paramsRaw...)
	owned := make([][]byte, len(originals))
	for i, raw := range originals {
		owned[i] = append([]byte(nil), raw...)
	}
	specs, err := MultiSourceArrays(operation, body, owned)
	if err != nil {
		return err
	}
	resolve := func(spec MultiSourceArraySpec) (objectstore.Object, error) {
		if err := ctx.Err(); err != nil {
			return objectstore.Object{}, err
		}
		return catalog.ResolveMultiSourceArray(ctx, who, tenant, project, snapshot, operation, append([]byte(nil), body...), append([]byte(nil), owned[spec.Ordinal]...), spec)
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
		verifyErr := VerifyPerspectiveSourceArray(ctx, spec.Array, object, reader)
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
