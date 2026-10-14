package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2input"
)

type multiOutputArray struct {
	ref            v2input.Ref
	path, contract string
	width, height  int64
	channels       int
	unit, binary   bool
	full           bool
}

func multiOutputArrays(d MultiResultDocuments) ([]multiOutputArray, error) {
	if !d.valid {
		return nil, v2input.ErrInvalid
	}
	image, err := d.Image()
	if err != nil {
		return nil, err
	}
	mask, err := d.Mask()
	if err != nil {
		return nil, err
	}
	imageRaw, maskRaw := []byte(d.image), []byte(d.mask)
	regs := make([][]byte, len(d.registrations))
	for i, r := range d.registrations {
		regs[i] = []byte(r)
	}
	_, _, geometry, err := ValidateMultiOutputGeometry(d.sources, imageRaw, maskRaw, regs)
	if err != nil {
		return nil, err
	}
	originals, err := d.sources.Images()
	if err != nil {
		return nil, err
	}
	_, reference, _, err := d.binding.Inputs()
	if err != nil {
		return nil, err
	}
	changedGeometry := image.Canvas.Width != originals[reference].Canvas.Width || image.Canvas.Height != originals[reference].Canvas.Height || geometry[reference].InverseField != nil
	identity := [9]float64{1, 0, 0, 0, 1, 0, 0, 0, 1}
	for i, value := range geometry[reference].Homography {
		if value != identity[i] {
			changedGeometry = true
		}
	}
	arrays := []multiOutputArray{
		{ref: v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}, path: "$/working_image/pixels", contract: "WorkingPixels", width: image.Canvas.Width, height: image.Canvas.Height, channels: 3},
		{ref: v2input.Ref{ArtifactID: image.Alpha.ID, SHA256: image.Alpha.SHA256}, path: "$/working_image/alpha", contract: "WorkingAlpha", width: image.Canvas.Width, height: image.Canvas.Height, unit: true},
		{ref: v2input.Ref{ArtifactID: mask.Artifact.ID, SHA256: mask.Artifact.SHA256}, path: "$/changed_mask/artifact", contract: "MaskPixels", width: image.Canvas.Width, height: image.Canvas.Height, unit: true, binary: true, full: changedGeometry},
	}
	for i, r := range geometry {
		if r.InverseField != nil {
			arrays = append(arrays, multiOutputArray{ref: *r.InverseField, path: "$/report/registrations/*/inverse_field", contract: "DenseWarpField", width: originals[i].Canvas.Width, height: originals[i].Canvas.Height, channels: 2})
		}
	}
	return arrays, nil
}

// Verifies output content, cylindrical inverse coordinates and final current
// authority; does not replay composition pixels or certify registration accuracy
// or photographic quality.
func VerifyAdoptedMultiOutputArrays(ctx context.Context, source AdoptedMultiCatalogueSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, d MultiResultDocuments, bucket string) error {
	return verifyAdoptedMultiOutputArrays(ctx, source, objects, who, tenant, project, producer, d, bucket, nil)
}

// capture is called only after every byte, locator, original and final root gate
// succeeds. A failure must never expose partial output witnesses.
func verifyAdoptedMultiOutputArrays(ctx context.Context, source AdoptedMultiCatalogueSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, producer uuid.UUID, d MultiResultDocuments, bucket string, capture func([]multiChildPin)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if source == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || !d.valid {
		return v2input.ErrInvalid
	}
	b := d.binding
	if err := validateAdoptedMultiBinding(b, producer, b.Operation, b.Result); err != nil {
		return err
	}
	arrays, err := multiOutputArrays(d)
	if err != nil {
		return err
	}
	pinned := &pinnedMultiCatalogueSource{AdoptedMultiCatalogueSource: source, binding: b, root: d.rootObject, pinned: true, children: append([]multiChildPin(nil), d.pins...)}
	root := func() error {
		_, _, err := pinned.LoadObject(ctx, who, tenant, project, producer, b.Operation, b.Result, bucket)
		return err
	}
	for _, array := range arrays {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := root(); err != nil {
			return err
		}
		obj, err := pinned.LoadChildObject(ctx, who, tenant, project, producer, b.Operation, b.Result, array.ref, array.path, array.contract, bucket)
		if err != nil {
			return err
		}
		media, limit, ok := multiChildObjectLimit(array.path, array.contract)
		components := int64(array.channels)
		if components == 0 {
			components = 1
		}
		if !ok || !validMultiChildObject(obj, array.ref, media, limit, false) || obj.Bytes > array.width*array.height*components*4+65536 {
			return v2input.ErrInvalid
		}
		reader, err := objects.Open(ctx, obj)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return err
		}
		if reader == nil {
			return v2input.ErrInvalid
		}
		var verifyErr error
		if array.full {
			verifyErr = masknpy.VerifyFullMaskNPY(ctx, reader, obj.Bytes, obj.SHA256, array.width, array.height)
		} else if array.contract == "DenseWarpField" {
			verifyErr = masknpy.VerifyCylindricalFieldNPY(ctx, reader, obj.Bytes, obj.SHA256, array.width, array.height)
		} else if array.binary {
			verifyErr = masknpy.VerifyBinaryMaskNPY(ctx, reader, obj.Bytes, obj.SHA256, array.width, array.height)
		} else {
			verifyErr = masknpy.VerifyFloat32ArrayNPY(ctx, reader, obj.Bytes, obj.SHA256, array.width, array.height, array.channels, array.unit)
		}
		closeErr := reader.Close()
		if errors.Is(verifyErr, masknpy.ErrInvalid) {
			return v2input.ErrInvalid
		}
		if verifyErr != nil {
			return verifyErr
		}
		if closeErr != nil {
			return closeErr
		}
		if _, err := pinned.LoadChildObject(ctx, who, tenant, project, producer, b.Operation, b.Result, array.ref, array.path, array.contract, bucket); err != nil {
			return err
		}
		if err := root(); err != nil {
			return err
		}
	}
	for _, pin := range pinned.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := pinned.LoadChildObject(ctx, who, tenant, project, producer, b.Operation, b.Result, pin.ref, pin.path, pin.contract, bucket); err != nil {
			return err
		}
	}
	fresh, err := source.LoadOriginalInventory(ctx, who, tenant, project, producer, b, bucket)
	if err != nil {
		return err
	}
	if !d.sources.originals.inventory.equal(fresh) {
		return v2input.ErrExpired
	}
	if err := root(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if capture != nil {
		capture(append([]multiChildPin(nil), pinned.children...))
	}
	return nil
}
