package v2render

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type AdoptedMultiCatalogueSource interface {
	AdoptedMultiChildSource
	AdoptedMultiOriginalInventorySource
}

// MultiResultDocuments proves typed JSON/source closure and cross-document
// geometry. Output NPY bytes/quality are deliberately not certified by this type.
// The caller still needs output-array verification before rendering or Apply.
type MultiResultDocuments struct {
	binding             AdoptedMultiBinding
	root                v2draft.ImageResultDocument
	sources             VerifiedMultiSourceClosure
	image, mask, report string
	registrations       []string
	valid               bool
	rootObject          objectstore.Object
	pins                []multiChildPin
}

func (v MultiResultDocuments) Binding() (AdoptedMultiBinding, error) {
	if !v.valid {
		return AdoptedMultiBinding{}, v2input.ErrInvalid
	}
	return v.binding, nil
}
func (v MultiResultDocuments) Image() (review.WorkingImage, error) {
	if !v.valid {
		return review.WorkingImage{}, v2input.ErrInvalid
	}
	return review.ParseWorkingImage([]byte(v.image))
}
func (v MultiResultDocuments) Mask() (review.MaskRef, error) {
	if !v.valid {
		return review.MaskRef{}, v2input.ErrInvalid
	}
	return review.ParseMaskRef([]byte(v.mask))
}
func (v MultiResultDocuments) Report() (MultiImageReport, error) {
	if !v.valid {
		return MultiImageReport{}, v2input.ErrInvalid
	}
	return ParseMultiImageReport([]byte(v.report), v.binding, v.root.Report)
}

type multiChildPin struct {
	ref            v2input.Ref
	path, contract string
	object         objectstore.Object
}
type pinnedMultiCatalogueSource struct {
	AdoptedMultiCatalogueSource
	root     objectstore.Object
	binding  AdoptedMultiBinding
	pinned   bool
	children []multiChildPin
}

func (s *pinnedMultiCatalogueSource) LoadObject(ctx context.Context, who auth.Identity, t, p, task uuid.UUID, op string, ref v2input.Ref, bucket string) (AdoptedMultiBinding, objectstore.Object, error) {
	b, o, err := s.AdoptedMultiCatalogueSource.LoadObject(ctx, who, t, p, task, op, ref, bucket)
	if err != nil {
		return b, o, err
	}
	if s.pinned && (s.root != o || s.binding != b) {
		return AdoptedMultiBinding{}, objectstore.Object{}, v2input.ErrExpired
	}
	s.root = o
	s.binding = b
	s.pinned = true
	return b, o, nil
}
func (s *pinnedMultiCatalogueSource) LoadChildObject(ctx context.Context, who auth.Identity, t, p, task uuid.UUID, op string, root, ref v2input.Ref, path, contract, bucket string) (objectstore.Object, error) {
	o, err := s.AdoptedMultiCatalogueSource.LoadChildObject(ctx, who, t, p, task, op, root, ref, path, contract, bucket)
	if err != nil {
		return o, err
	}
	for _, pin := range s.children {
		if pin.ref == ref && pin.path == path && pin.contract == contract {
			if pin.object != o {
				return objectstore.Object{}, v2input.ErrExpired
			}
			return o, nil
		}
	}
	s.children = append(s.children, multiChildPin{ref: ref, path: path, contract: contract, object: o})
	return o, nil
}

func LoadAdoptedMultiDocuments(ctx context.Context, source AdoptedMultiCatalogueSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, operation string, expected v2input.Ref, bucket string) (MultiResultDocuments, error) {
	empty := MultiResultDocuments{}
	if source == nil || objects == nil {
		return empty, v2input.ErrInvalid
	}
	pinned := &pinnedMultiCatalogueSource{AdoptedMultiCatalogueSource: source}
	binding, root, err := LoadAdoptedMultiResult(ctx, pinned, objects, who, tenant, project, producer, operation, expected, bucket)
	if err != nil {
		return empty, err
	}
	originals, err := LoadAdoptedMultiSourceClosure(ctx, pinned, objects, who, tenant, project, producer, binding, bucket)
	if err != nil {
		return empty, err
	}
	load := func(ref v2input.Ref, path, contract string) ([]byte, error) {
		return LoadAdoptedMultiJSONChild(ctx, pinned, objects, who, tenant, project, producer, binding, ref, path, contract, bucket)
	}
	image, err := load(root.WorkingImage, "$/working_image", "WorkingImage")
	if err != nil {
		return empty, err
	}
	mask, err := load(root.ChangedMask, "$/changed_mask", "MaskRef")
	if err != nil {
		return empty, err
	}
	reportRaw, err := load(root.Report, "$/report", "ImageReport")
	if err != nil {
		return empty, err
	}
	report, err := ParseMultiImageReport(reportRaw, binding, root.Report)
	if err != nil {
		return empty, err
	}
	registrations := make([][]byte, len(report.Registrations))
	for i, ref := range report.Registrations {
		registrations[i], err = load(ref, "$/report/registrations/*", "Artifact")
		if err != nil {
			return empty, err
		}
	}
	output, changed, geometry, err := ValidateMultiOutputGeometry(originals, image, mask, registrations)
	if err != nil {
		return empty, err
	}
	// JSON artifacts and physical outputs must never alias one another or sources.
	used := map[uuid.UUID]bool{binding.Result.ArtifactID: true}
	_, _, calibration, err := binding.Inputs()
	if err != nil {
		return empty, err
	}
	used[calibration.ArtifactID] = true
	for _, item := range originals.originals.inventory.items {
		used[item.Ref.ArtifactID] = true
	}
	add := func(id uuid.UUID) bool {
		if used[id] {
			return false
		}
		used[id] = true
		return true
	}
	for _, ref := range []v2input.Ref{root.WorkingImage, root.ChangedMask, root.Report} {
		if !add(ref.ArtifactID) {
			return empty, v2input.ErrInvalid
		}
	}
	for _, ref := range report.Registrations {
		if !add(ref.ArtifactID) {
			return empty, v2input.ErrInvalid
		}
	}
	for _, id := range []uuid.UUID{output.Pixels.ID, output.Alpha.ID, changed.Artifact.ID} {
		if !add(id) {
			return empty, v2input.ErrInvalid
		}
	}
	for _, registration := range geometry {
		if registration.InverseField != nil && !add(registration.InverseField.ArtifactID) {
			return empty, v2input.ErrInvalid
		}
	}
	// Revalidate all earlier children after later IO; a local per-child check is
	// insufficient to establish a final coherent catalogue snapshot.
	for _, pin := range pinned.children {
		if err := ctx.Err(); err != nil {
			return empty, err
		}
		if _, err := pinned.LoadChildObject(ctx, who, tenant, project, producer, operation, expected, pin.ref, pin.path, pin.contract, bucket); err != nil {
			return empty, err
		}
	}
	fresh, err := source.LoadOriginalInventory(ctx, who, tenant, project, producer, binding, bucket)
	if err != nil {
		return empty, err
	}
	if !originals.originals.inventory.equal(fresh) {
		return empty, v2input.ErrExpired
	}
	if _, _, err := pinned.LoadObject(ctx, who, tenant, project, producer, operation, expected, bucket); err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	docs := make([]string, len(registrations))
	for i, raw := range registrations {
		docs[i] = string(raw)
	}
	return MultiResultDocuments{binding: binding, root: root, sources: originals, image: string(image), mask: string(mask), report: string(reportRaw), registrations: docs, valid: true, rootObject: pinned.root, pins: append([]multiChildPin(nil), pinned.children...)}, nil
}
