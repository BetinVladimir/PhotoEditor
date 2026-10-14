package workflow

import (
	"bytes"
	"context"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2optical"
)

type PerspectiveLinesSourceCatalog interface {
	ResolveSource(context.Context, PerspectiveLinesScope, uuid.UUID, auth.Identity) (objectstore.Object, error)
}
type PerspectiveLinesArrayWitness struct {
	Spec   v2optical.PerspectiveSourceArraySpec
	Object objectstore.Object
}
type VerifiedPerspectiveLinesInputs struct {
	scope          PerspectiveLinesScope
	task, snapshot uuid.UUID
	who            auth.Identity
	source         objectstore.Object
	arrays         []PerspectiveLinesArrayWitness
}

func (v VerifiedPerspectiveLinesInputs) Source() objectstore.Object { return v.source }
func (v VerifiedPerspectiveLinesInputs) Arrays() []PerspectiveLinesArrayWitness {
	return append([]PerspectiveLinesArrayWitness(nil), v.arrays...)
}

// This verifies only current RGB/alpha pixels. Historical dense fields remain
// declared provenance and are not sampled by the line detector. SQL admission
// must repeat current authorization and compare every returned immutable locator.
func VerifyPerspectiveLinesInputs(ctx context.Context, sources PerspectiveLinesSourceCatalog, catalog v2optical.PerspectiveSourceArrayCatalog, objects v2input.SessionAnalysisObjectReader, plan PerspectiveLinesPlan, snapshot uuid.UUID, who auth.Identity) (VerifiedPerspectiveLinesInputs, error) {
	var zero VerifiedPerspectiveLinesInputs
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if sources == nil || catalog == nil || objects == nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := CompilePerspectiveLinesIntent(plan, snapshot, who); err != nil {
		return zero, err
	}
	owned, err := CompilePerspectiveLinesPlan(plan.scope, plan.image, plan.workingICC)
	if err != nil {
		return zero, err
	}
	params := v2optical.PerspectiveParams{SchemaVersion: 1, Base: owned.scope.Base, Mode: "manual", Strength: 0, Corners: []v2optical.PerspectivePoint{{0, 0}, {1, 0}, {1, 1}, {0, 1}}, MaxCropFraction: 1}
	source, err := sources.ResolveSource(ctx, owned.scope, snapshot, who)
	if err != nil {
		return zero, err
	}
	if source.Key == "" || len(source.Key) > 1024 || source.VersionID == "" || source.VersionID == "null" || len(source.VersionID) > 1024 || source.SHA256 != owned.scope.Base.Source.SHA256 || source.Bytes != int64(len(owned.image)) || source.ContentType != "application/json" {
		return zero, v2input.ErrInvalid
	}
	stream, err := objects.Open(ctx, source)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return zero, err
	}
	if stream == nil {
		return zero, v2input.ErrInvalid
	}
	raw, readErr := io.ReadAll(io.LimitReader(stream, source.Bytes+1))
	closeErr := stream.Close()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	if !bytes.Equal(raw, owned.image) {
		return zero, v2input.ErrInvalid
	}
	specs, err := v2optical.PerspectiveSourceArrays(params, owned.image)
	if err != nil {
		return zero, err
	}
	witnesses := []PerspectiveLinesArrayWitness{}
	resolve := func(spec v2optical.PerspectiveSourceArraySpec) (objectstore.Object, error) {
		p := params
		p.Corners = append([]v2optical.PerspectivePoint(nil), params.Corners...)
		return catalog.ResolvePerspectiveSourceArray(ctx, who, owned.scope.Tenant, owned.scope.Project, snapshot, p, bytes.Clone(owned.image), spec)
	}
	for _, spec := range specs {
		if spec.Role != "WorkingPixels" && spec.Role != "WorkingAlpha" {
			continue
		}
		object, err := resolve(spec)
		if err != nil {
			return zero, err
		}
		components := int64(spec.Channels)
		if spec.Alpha {
			components = 1
		}
		if object.Key == "" || len(object.Key) > 1024 || object.VersionID == "" || object.VersionID == "null" || len(object.VersionID) > 1024 || object.SHA256 != spec.Ref.SHA256 || object.Bytes < 1 || object.Bytes > spec.Width*spec.Height*components*4+65536 || (object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream") {
			return zero, v2input.ErrInvalid
		}
		stream, err := objects.Open(ctx, object)
		if err != nil {
			if stream != nil {
				_ = stream.Close()
			}
			return zero, err
		}
		if stream == nil {
			return zero, v2input.ErrInvalid
		}
		verifyErr := v2optical.VerifyPerspectiveSourceArray(ctx, spec, object, stream)
		closeErr := stream.Close()
		if verifyErr != nil {
			return zero, verifyErr
		}
		if closeErr != nil {
			return zero, closeErr
		}
		current, err := resolve(spec)
		if err != nil {
			return zero, err
		}
		if current != object {
			return zero, v2input.ErrExpired
		}
		witnesses = append(witnesses, PerspectiveLinesArrayWitness{Spec: spec, Object: object})
	}
	for _, w := range witnesses {
		current, err := resolve(w.Spec)
		if err != nil {
			return zero, err
		}
		if current != w.Object {
			return zero, v2input.ErrExpired
		}
	}
	current, err := sources.ResolveSource(ctx, owned.scope, snapshot, who)
	if err != nil {
		return zero, err
	}
	if current != source {
		return zero, v2input.ErrExpired
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	return VerifiedPerspectiveLinesInputs{scope: owned.scope, task: owned.task, snapshot: snapshot, who: who, source: source, arrays: witnesses}, nil
}
