package v2region

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// AuthoredRegionIntent is server-resolved immutable intent, not a public body or
// authority grant. Renderer pins are selected by the actual installation.
// Source-to-target geometry must still be proved by the executed full render.
type AuthoredRegionIntent struct {
	Tenant          uuid.UUID                       `json:"tenant_id"`
	Project         uuid.UUID                       `json:"project_id"`
	Asset           uuid.UUID                       `json:"asset_id"`
	Actor           uuid.UUID                       `json:"actor_id"`
	Session         uuid.UUID                       `json:"session_id"`
	DraftDocument   uuid.UUID                       `json:"draft_document_id"`
	DraftRevision   int64                           `json:"draft_revision"`
	FrozenDocument  uuid.UUID                       `json:"frozen_document_id"`
	Binding         v2input.SessionVersionBinding   `json:"source"`
	Recipe          v2input.Ref                     `json:"recipe"`
	Rect            RegionRect                      `json:"rect"`
	MaxEdge         int                             `json:"max_edge"`
	TargetICCSHA    string                          `json:"target_icc_sha256"`
	PresentationRuntimeSHA string `json:"presentation_runtime_sha256"`
	PresentationICCLockSHA string `json:"presentation_icc_lock_sha256"`
	RenderPolicySHA string                          `json:"render_policy_sha256"`
	Qualification   v2render.RenderQualificationPin `json:"qualification"`
}

// SHA256 binds cache/dedup to exact authored intent and qualified execution.
// Reauthorization is mandatory even when this content key already exists.
func (i AuthoredRegionIntent) SHA256() (string, error) {
	for _, id := range []uuid.UUID{i.Tenant, i.Project, i.Asset, i.Actor, i.Session, i.DraftDocument, i.FrozenDocument, i.Binding.AssetVersionID, i.Binding.Source.ArtifactID, i.Recipe.ArtifactID} {
		if id == uuid.Nil {
			return "", ErrRegionInvalid
		}
	}
	if i.DraftRevision < 1 || i.DraftDocument == i.FrozenDocument || i.Binding.Source.ArtifactID == i.Recipe.ArtifactID {
		return "", ErrRegionInvalid
	}
	for _, pin := range []string{i.Binding.Source.SHA256, i.Binding.CanvasHash, i.Recipe.SHA256, i.TargetICCSHA, i.PresentationRuntimeSHA, i.PresentationICCLockSHA, i.RenderPolicySHA, i.Qualification.ModelSHA, i.Qualification.RuntimeSHA, i.Qualification.DeviceSHA, i.Qualification.DatasetSHA, i.Qualification.CalibrationSHA} {
		if !validRegionSHA(pin) {
			return "", ErrRegionInvalid
		}
	}
	if i.Qualification.Backend != "cpu" && i.Qualification.Backend != "cuda" && i.Qualification.Backend != "mps" {
		return "", ErrRegionInvalid
	}
	if _, err := CompileRegionGeometry(1, 1, i.Rect, i.MaxEdge); err != nil {
		return "", err
	}
	return canonical.Hash(i)
}

// Content keys bind exact recipe/geometry/ROI/ICC/qualification for cache lookup.
// Actual child admissions additionally use RequestChildKey: separate requests
// must not accidentally share cancellation or collide on a unique child run.
func (i AuthoredRegionIntent) ChildKey(phase string) (string, error) {
	if phase != "render" && phase != "crop" {
		return "", ErrRegionInvalid
	}
	hash, err := i.SHA256()
	if err != nil {
		return "", err
	}
	return "authored-region:" + hash + ":" + phase, nil
}

// RequestChildKey retains exact content identity while separating durable
// cancellation ownership. A retry at the same admission key recovers the same
// request and children. A new request may use existing verified content cache,
// but cannot inherit another request's mutable run or cancellation state.
func(i AuthoredRegionIntent)RequestChildKey(request uuid.UUID,phase string)(string,error){
 if request==uuid.Nil{return "",ErrRegionInvalid}
 content,err:=i.ChildKey(phase);if err!=nil{return "",err}
 return content+":"+request.String(),nil
}
