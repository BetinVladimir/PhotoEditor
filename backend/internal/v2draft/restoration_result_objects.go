package v2draft

import (
	"context"
	"io"

	"github.com/google/uuid"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// RestorationArrayScope is a server-issued task/result scope. Implementations
// must resolve only registered input and result closure members for this scope.
type RestorationArrayScope struct {
	Tenant  uuid.UUID `json:"tenant"`
	Task    uuid.UUID `json:"task"`
	Result  uuid.UUID `json:"result"`
	Root    uuid.UUID `json:"root"`
	RootSHA string    `json:"root_sha"`
}

type RestorationArrayCatalog interface {
	ResolveRestorationArray(context.Context, RestorationArrayScope, string, v2input.Ref) (objectstore.Object, error)
}

// VerifyRestorationPublishedResult is the adoption-facing composite: it first
// proves the canonical review-only document closure, then physically verifies
// every versioned array belonging to that exact root result.
func VerifyRestorationPublishedResult(ctx context.Context, scope RestorationArrayScope,
	catalog RestorationArrayCatalog, objects v2input.SessionAnalysisObjectReader,
	operation string, paramsRaw, targetRaw, protectedRaw []byte,
	root v2input.Ref, docs RestorationResultDocuments) (RestorationReviewCandidate, int64, error) {
	var zero RestorationReviewCandidate
	if root.ArtifactID != scope.Root || root.SHA256 != scope.RootSHA {
		return zero, 0, v2input.ErrInvalid
	}
	candidate, err := VerifyRestorationResultDocuments(ctx, operation, paramsRaw, root, docs)
	if err != nil {
		return zero, 0, err
	}
	refs, err := RestorationResultArrayRefs(ctx, operation, paramsRaw, docs.Source,
		targetRaw, protectedRaw, candidate)
	if err != nil {
		return zero, 0, err
	}
	count, err := VerifyRestorationResultObjects(ctx, scope, catalog, objects,
		operation, paramsRaw, docs.Source, targetRaw, protectedRaw, candidate, refs)
	if err != nil {
		return zero, 0, err
	}
	return candidate, count, nil
}

// VerifyRestorationResultObjects reads exact versioned NPY objects and repeats
// catalog authorization after streaming. Neither a caller-supplied key nor a
// mutable latest version can be used as evidence for result adoption.
func VerifyRestorationResultObjects(ctx context.Context, scope RestorationArrayScope,
	catalog RestorationArrayCatalog, objects v2input.SessionAnalysisObjectReader,
	operation string, paramsRaw, sourceRaw, targetRaw, protectedRaw []byte,
	candidate RestorationReviewCandidate,
	refs RestorationPixelStreams) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if scope.Tenant == uuid.Nil || scope.Task == uuid.Nil || scope.Result == uuid.Nil ||
		scope.Root == uuid.Nil ||
		catalog == nil || objects == nil || !isDocumentRef(candidate.Result) ||
		candidate.Result.ArtifactID != scope.Root || candidate.Result.SHA256 != scope.RootSHA {
		return 0, v2input.ErrInvalid
	}
	type entry struct {
		role string
		pin  *RestorationPinnedArray
		obj  objectstore.Object
		body io.ReadCloser
	}
	items := []entry{{role: "source_pixels", pin: &refs.Source},
		{role: "output_pixels", pin: &refs.Output},
		{role: "target_pixels", pin: &refs.Target},
		{role: "protected_pixels", pin: &refs.Protected},
		{role: "changed_pixels", pin: &refs.Changed}}
	if refs.Generated != nil {
		items = append(items, entry{role: "generated_pixels", pin: refs.Generated})
	}
	defer func() {
		for i := range items {
			if items[i].body != nil {
				_ = items[i].body.Close()
			}
		}
	}()
	for i := range items {
		item := &items[i]
		if !isDocumentRef(item.pin.Ref) {
			return 0, v2input.ErrInvalid
		}
		object, err := catalog.ResolveRestorationArray(ctx, scope, item.role, item.pin.Ref)
		if err != nil {
			return 0, err
		}
		if !validRestorationArrayObject(object, item.pin.Ref) {
			return 0, v2input.ErrInvalid
		}
		body, err := objects.Open(ctx, object)
		if err != nil {
			return 0, err
		}
		if body == nil {
			return 0, v2input.ErrInvalid
		}
		item.obj, item.body = object, body
		item.pin.Data = masknpy.PinnedMask{Reader: body, Bytes: object.Bytes, SHA256: object.SHA256}
	}
	count, err := VerifyRestorationResultPixels(ctx, operation, paramsRaw, sourceRaw,
		targetRaw, protectedRaw, candidate, refs)
	if err != nil {
		return 0, err
	}
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return 0, err
		}
		current, err := catalog.ResolveRestorationArray(ctx, scope, item.role, item.pin.Ref)
		if err != nil {
			return 0, err
		}
		if current != item.obj {
			return 0, v2input.ErrExpired
		}
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	return count, nil
}

func validRestorationArrayObject(object objectstore.Object, ref v2input.Ref) bool {
	return object.Key != "" && len(object.Key) <= 1024 &&
		object.VersionID != "" && object.VersionID != "null" && len(object.VersionID) <= 1024 &&
		object.SHA256 == ref.SHA256 && object.ContentType == "application/x-npy" &&
		object.Bytes >= 14 && object.Bytes <= 2400065536
}
