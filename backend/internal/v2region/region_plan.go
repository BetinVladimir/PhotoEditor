package v2region

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2learning"
)

// FrozenRegionPlan is byte-stable preparation evidence, not job admission.
// A transaction must recheck live authority and resolve every nested input
// before a worker can receive this plan. JSON returns a defensive copy.
type FrozenRegionPlan struct {
	body     []byte
	digest   string
	inputs   []v2input.Entry
	canvas   review.Canvas
	hasAlpha bool
}

func (p FrozenRegionPlan) JSON() []byte   { return append([]byte(nil), p.body...) }
func (p FrozenRegionPlan) SHA256() string { return p.digest }

// decode verifies the exact frozen transport bytes, not a reserialized model
// that would erase whitespace or unknown fields before hashing.
func (p FrozenRegionPlan) decode() (regionPlanDocument, error) {
	var document regionPlanDocument
	actual := sha256.Sum256(p.body)
	if hex.EncodeToString(actual[:]) != p.digest {
		return document, ErrRegionInvalid
	}
	canonicalBody, err := canonical.Bytes(p.body)
	if err != nil || !bytes.Equal(canonicalBody, p.body) || json.Unmarshal(p.body, &document) != nil {
		return document, ErrRegionInvalid
	}
	return document, nil
}

// InputRequirements declares validated image descendants and recipe roots.
// The base style still requires scoped resolution of any nested references.
// Every entry must be authorized and version-pinned before job admission.
func (p FrozenRegionPlan) InputRequirements() []v2input.Entry {
	return append([]v2input.Entry(nil), p.inputs...)
}

type regionObjectPin struct {
	Ref     v2input.Ref `json:"ref"`
	Bucket  string      `json:"bucket"`
	Key     string      `json:"object_key"`
	Version string      `json:"object_version"`
	Bytes   int64       `json:"bytes"`
}
type regionPlanDocument struct {
    Execution *RegionExecutionDescriptor `json:"execution,omitempty"`
	Inputs        []v2input.Entry         `json:"inputs"`
	SchemaVersion int                     `json:"schema_version"`
	TenantID      uuid.UUID               `json:"tenant_id"`
	ProjectID     uuid.UUID               `json:"project_id"`
	AssetID       uuid.UUID               `json:"asset_id"`
	Request       RegionPreviewRequest    `json:"request"`
	Source        regionObjectPin         `json:"source"`
	Recipe        regionObjectPin         `json:"recipe"`
	FrozenRecipe  v2learning.FrozenRecipe `json:"frozen_recipe"`
	Geometry      RegionGeometry          `json:"geometry"`
	RenderOrder   string                  `json:"render_order"`
}

func freezeRegionPlan(tenant, asset, project uuid.UUID, bucket string, req RegionPreviewRequest, source, recipe RegionSourceMetadata, evidence CompiledRegionEvidence) (FrozenRegionPlan, error) {
	if tenant == uuid.Nil || asset == uuid.Nil || project == uuid.Nil || bucket == "" {
		return FrozenRegionPlan{}, ErrRegionInvalid
	}
	pin := func(m RegionSourceMetadata) regionObjectPin {
		return regionObjectPin{m.Ref, bucket, m.Object.Key, m.Object.VersionID, m.Object.Bytes}
	}
	inputs, err := regionInputRequirements(source.Ref, recipe.Ref, evidence)
	if err != nil {
		return FrozenRegionPlan{}, err
	}
	// Global recipe operators must see the original canvas before cropping.
	document := regionPlanDocument{Inputs:inputs,SchemaVersion:1,TenantID:tenant,ProjectID:project,AssetID:asset,Request:req,Source:pin(source),Recipe:pin(recipe),FrozenRecipe:evidence.Recipe,Geometry:evidence.Geometry,RenderOrder:"full_canvas_then_crop_then_downscale"}
	body, err := canonical.Marshal(document)
	if err != nil {
		return FrozenRegionPlan{}, ErrRegionInvalid
	}
	digest, err := canonical.Hash(document)
	if err != nil {
		return FrozenRegionPlan{}, ErrRegionInvalid
	}
	return FrozenRegionPlan{body: body, digest: digest, inputs: inputs, canvas: evidence.Source.Canvas, hasAlpha: evidence.Source.Alpha != nil}, nil
}

// RegionSourceShape is verified preparation evidence for full-canvas budgeting,
// not a request or worker wire contract. ROI dimensions cannot substitute for it.
type RegionSourceShape struct {
	Width, Height int64
	HasAlpha      bool
}

func (p FrozenRegionPlan) SourceShape() (RegionSourceShape, error) {
	document, err := p.decode()
	if err != nil {
		return RegionSourceShape{}, err
	}
	digest, err := canonical.Hash(p.canvas)
	if err != nil || digest != document.FrozenRecipe.Source.CanvasHash {
		return RegionSourceShape{}, ErrRegionInvalid
	}
	geometry, err := CompileRegionGeometry(p.canvas.Width, p.canvas.Height, document.Request.Rect, document.Request.MaxEdge)
	if err != nil || geometry != document.Geometry {
		return RegionSourceShape{}, ErrRegionInvalid
	}
	return RegionSourceShape{p.canvas.Width, p.canvas.Height, p.hasAlpha}, nil
}
