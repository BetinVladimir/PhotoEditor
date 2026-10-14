package v2draft

import (
	"context"
	"sort"

	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RestorationVerifiedObject struct {
	Kind    string      `json:"kind"`
	Role    string      `json:"role"`
	Ref     v2input.Ref `json:"ref"`
	Key     string      `json:"key"`
	Version string      `json:"version"`
	Bytes   int64       `json:"bytes"`
	Media   string      `json:"media_type"`
}

type RestorationVerificationEvidence struct {
	Scope         RestorationArrayScope       `json:"scope"`
	Bucket        string                      `json:"bucket"`
	Operation     string                      `json:"operation"`
	ChangedPixels int64                       `json:"changed_pixels"`
	Candidate     RestorationReviewCandidate  `json:"candidate"`
	Objects       []RestorationVerifiedObject `json:"objects"`
	verified      bool
}

type restorationLocatorRecorder struct {
	documents RestorationResultDocumentCatalog
	arrays    RestorationArrayCatalog
	first     map[string]objectstore.Object
	counts    map[string]int
	records   map[string]RestorationVerifiedObject
}

func newRestorationLocatorRecorder(documents RestorationResultDocumentCatalog,
	arrays RestorationArrayCatalog) *restorationLocatorRecorder {
	return &restorationLocatorRecorder{documents: documents, arrays: arrays,
		first: map[string]objectstore.Object{}, counts: map[string]int{},
		records: map[string]RestorationVerifiedObject{}}
}

func (r *restorationLocatorRecorder) resolve(ctx context.Context, scope RestorationArrayScope,
	kind, role string, ref v2input.Ref) (objectstore.Object, error) {
	var object objectstore.Object
	var err error
	if kind == "document" {
		object, err = r.documents.ResolveRestorationDocument(ctx, scope, role, ref)
	} else {
		object, err = r.arrays.ResolveRestorationArray(ctx, scope, role, ref)
	}
	if err != nil {
		return objectstore.Object{}, err
	}
	key := kind + "/" + role
	if previous, found := r.first[key]; found {
		if previous != object || r.records[key].Ref != ref {
			return objectstore.Object{}, v2input.ErrExpired
		}
	} else {
		r.first[key] = object
		r.records[key] = RestorationVerifiedObject{Kind: kind, Role: role,
			Ref: ref, Key: object.Key, Version: object.VersionID,
			Bytes: object.Bytes, Media: object.ContentType}
	}
	r.counts[key]++
	return object, nil
}

func (r *restorationLocatorRecorder) ResolveRestorationDocument(ctx context.Context,
	scope RestorationArrayScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	return r.resolve(ctx, scope, "document", role, ref)
}

func (r *restorationLocatorRecorder) ResolveRestorationArray(ctx context.Context,
	scope RestorationArrayScope, role string, ref v2input.Ref) (objectstore.Object, error) {
	return r.resolve(ctx, scope, "array", role, ref)
}

func (r *restorationLocatorRecorder) evidence(scope RestorationArrayScope, bucket string,
	operation string, candidate RestorationReviewCandidate, changed int64,
) (RestorationVerificationEvidence, error) {
	if len(r.records) < 10 || len(r.records) > 12 {
		return RestorationVerificationEvidence{}, v2input.ErrInvalid
	}
	keys := make([]string, 0, len(r.records))
	for key := range r.records {
		if r.counts[key] != 2 {
			return RestorationVerificationEvidence{}, v2input.ErrInvalid
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)
	objects := make([]RestorationVerifiedObject, 0, len(keys))
	for _, key := range keys {
		objects = append(objects, r.records[key])
	}
	return RestorationVerificationEvidence{Scope: scope, Bucket: bucket, Operation: operation,
		ChangedPixels: changed, Candidate: candidate, Objects: objects, verified: true}, nil
}
