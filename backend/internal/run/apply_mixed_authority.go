package run

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2qualification"
)

func applyMixedRequirements(r applyRuntimeSelection, n styleExecutionNeeds) map[string][2]string {
	out := map[string][2]string{}
	add := func(op, backend, lock string) {
		if backend != "" {
			out[op] = [2]string{backend, lock}
		}
	}
	if r.PersonRequired {
		add("mask.person", r.PersonBackend, r.PersonLock)
	}
	if n.Geometry.Enabled {
		add("pose.detect", r.PersonBackend, r.PersonLock)
	}
	if n.Beauty.Enabled || n.Defects.Enabled {
		for _, op := range []string{"face.detect", "face.landmarks", "face.parse"} {
			add(op, r.PersonBackend, r.PersonLock)
		}
	}
	if n.Extensions.Background.Mode == "blur" {
		add("depth.predict", r.PersonBackend, r.PersonLock)
	}
	if n.Extensions.Culling.Enabled {
		add("features.scene", r.PersonBackend, r.PersonLock)
	}
	if n.Extensions.Restoration.Denoise == denoiseModelID {
		add("image.denoise", r.RestorationBackend, r.RestorationLock)
	}
	if n.Extensions.Restoration.Upscale == "2x" || n.Extensions.Restoration.Upscale == "4x" {
		add("image.upscale", r.RestorationBackend, r.RestorationLock)
	}
	if n.Extensions.Inpainting.Enabled {
		add("image.inpaint", r.CreativeBackend, r.CreativeLock)
	}
	if n.Extensions.Generation.Enabled || n.Extensions.Background.Mode == "generate" {
		add("image.generate", r.CreativeBackend, r.CreativeLock)
	}
	if n.Extensions.Composition.Enabled && n.Extensions.Composition.OutpaintPolicy == "review" {
		add("image.outpaint", r.CreativeBackend, r.CreativeLock)
	}
	return out
}
func bindApplyMixedAuthority(ctx context.Context, tx applyRuntimeQuery, tenant, project uuid.UUID, r applyRuntimeSelection, n styleExecutionNeeds, raw []byte) ([]byte, error) {
	if r.PersonBackend != "mps" && r.RestorationBackend != "mps" && r.CreativeBackend != "mps" {
		return raw, nil
	}
	a, e := v2qualification.ResolveMixed(ctx, tx, tenant, project)
	if e != nil {
		return nil, e
	}
	for op, want := range applyMixedRequirements(r, n) {
		if _, e = a.Child(op, "1", want[0], want[1]); e != nil {
			return nil, domain.NewError("MIXED_RENDERER_QUALIFICATION_UNAVAILABLE", 409)
		}
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil || config == nil {
		return nil, v2qualification.ErrUnqualified
	}
	if _, found := config["mixed_renderer_authority"]; found {
		return nil, v2qualification.ErrUnqualified
	}
	bound, e := a.Canonical()
	if e != nil {
		return nil, e
	}
	config["mixed_renderer_authority"] = bound
	return canonical.Marshal(config)
}
