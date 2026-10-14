package run

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/project"
	"photoeditor/backend/internal/tenant"
)

//go:embed identity_style.json
var identityStyleJSON []byte

type ApplyRequest struct {
	SourceRevision      int64           `json:"source_revision"`
	StyleVersionID      uuid.UUID       `json:"style_version_id"`
	Scope               string          `json:"scope"`
	ItemIDs             []uuid.UUID     `json:"item_ids,omitempty"`
	SelectionSnapshotID *uuid.UUID      `json:"selection_snapshot_id,omitempty"`
	Strength            *float64        `json:"strength,omitempty"`
	OverridesID         *uuid.UUID      `json:"overrides_id,omitempty"`
	JobOverrides        json.RawMessage `json:"job_overrides,omitempty"`
	OutputName          string          `json:"output_name,omitempty"`
}
type CreateRequest struct {
	Kind               string    `json:"kind"`
	SourceCollectionID uuid.UUID `json:"source_collection_id"`
	ApplyRequest
}
type ApplyAccepted struct {
	RunID              uuid.UUID `json:"run_id"`
	OutputCollectionID uuid.UUID `json:"output_collection_id"`
	Status             string    `json:"status"`
}

type seriesApplyQualification struct {
	ModelLockSHA     string
	RuntimeLockSHA   string
	DeviceProfileSHA string
}

// BoundItemOverride is an immutable, source-bound renderer override for one
// selected collection item. Only the internal analysis Apply path accepts it.
type BoundItemOverride struct {
	ItemID         uuid.UUID       `json:"item_id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA256    string          `json:"input_sha256"`
	Body           json.RawMessage `json:"body"`
}
type SaveEditsRequest struct {
	SourceRevision  int64       `json:"source_revision"`
	Scope           string      `json:"scope"`
	SelectedItemIDs []uuid.UUID `json:"selected_item_ids,omitempty"`
	OverridesID     uuid.UUID   `json:"overrides_id"`
	StyleVersionID  *uuid.UUID  `json:"style_version_id,omitempty"`
	OutputName      string      `json:"output_name,omitempty"`
}
type applySelection struct {
	ItemID         uuid.UUID `json:"item_id"`
	AssetID        uuid.UUID `json:"asset_id"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	ArtifactID     uuid.UUID `json:"artifact_id"`
	SHA256         string    `json:"sha256"`
	Position       int64     `json:"position"`
	OriginalBytes  int64     `json:"-"`
}

func isLowerHexSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func validateBoundItemOverrides(items []applySelection, input []BoundItemOverride) ([]BoundItemOverride, json.RawMessage, error) {
	invalid := func() ([]BoundItemOverride, json.RawMessage, error) {
		return nil, nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if len(items) == 0 || len(items) != len(input) || len(input) > 5000 {
		return invalid()
	}
	byItem := make(map[uuid.UUID]applySelection, len(items))
	for _, item := range items {
		byItem[item.ItemID] = item
	}
	seen := make(map[uuid.UUID]bool, len(input))
	var beauty json.RawMessage
	totalBytes := 0
	result := make([]BoundItemOverride, len(input))
	for i, entry := range input {
		item, found := byItem[entry.ItemID]
		if !found || entry.ItemID == uuid.Nil || entry.AssetVersionID == uuid.Nil ||
			seen[entry.ItemID] || entry.AssetVersionID != item.AssetVersionID ||
			entry.InputSHA256 != item.SHA256 || !isLowerHexSHA(entry.InputSHA256) ||
			len(entry.Body) < 2 || len(entry.Body) > 1<<20 {
			return invalid()
		}
		seen[entry.ItemID] = true
		body, err := canonical.Bytes(entry.Body)
		if err != nil || !bytes.Equal(body, entry.Body) || body[0] != '{' {
			return invalid()
		}
		totalBytes += len(body)
		if totalBytes > 8<<20 {
			return invalid()
		}
		var fields map[string]json.RawMessage
		if json.Unmarshal(body, &fields) != nil || len(fields) != 4 ||
			string(fields["schema_version"]) != "1" || string(fields["revision"]) != "1" {
			return invalid()
		}
		var sha string
		if json.Unmarshal(fields["input_sha256"], &sha) != nil || sha != entry.InputSHA256 {
			return invalid()
		}
		var modules map[string]json.RawMessage
		if json.Unmarshal(fields["modules"], &modules) != nil || len(modules) == 0 || len(modules) > 2 {
			return invalid()
		}
		for name, value := range modules {
			if name != "global" && name != "beauty" || len(value) == 0 || string(value) == "null" {
				return invalid()
			}
			if name == "beauty" {
				var flag struct {
					Enabled bool `json:"enabled"`
				}
				if json.Unmarshal(value, &flag) != nil {
					return invalid()
				}
				if flag.Enabled && len(beauty) == 0 {
					beauty = append(json.RawMessage(nil), value...)
				}
			}
		}
		result[i] = BoundItemOverride{entry.ItemID, entry.AssetVersionID,
			entry.InputSHA256, body}
	}
	sort.Slice(result, func(i, j int) bool {
		return bytes.Compare(result[i].ItemID[:], result[j].ItemID[:]) < 0
	})
	return result, beauty, nil
}

func analysisGraphStyle(style json.RawMessage, beauty json.RawMessage) (json.RawMessage, error) {
	if len(beauty) == 0 {
		return nil, nil
	}
	var document map[string]json.RawMessage
	if json.Unmarshal(style, &document) != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	var base struct {
		Enabled bool `json:"enabled"`
	}
	if json.Unmarshal(document["beauty"], &base) != nil {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if base.Enabled {
		return nil, nil
	}
	document["beauty"] = beauty
	return canonical.Marshal(document)
}

const personModelID = "lraspp_mobilenet_v3_large"
const personWeightID = "LRASPP_MobileNet_V3_Large_Weights.COCO_WITH_VOC_LABELS_V1"
const poseModelID = "keypointrcnn_resnet50_fpn"
const poseWeightID = "KeypointRCNN_ResNet50_FPN_Weights.COCO_V1"
const faceDetectModelID = "sfd"
const faceDetectWeightID = "face_alignment.s3fd-619a316812.pth"
const faceLandmarkModelID = "fan2d"
const faceLandmarkWeightID = "face_alignment.2DFAN4-cd938726ad.pth.tar"
const faceParseModelID = "bisenet_face19"
const faceParseWeightID = "zllrunning/79999_iter.pth.torchscript"
const denoiseModelID = "scunet_color_real_psnr"
const denoiseWeightID = "scunet_color_real_psnr.pth"
const upscale2ModelID = "realesrgan_x2"
const upscale2WeightID = "RealESRGAN_x2plus.pth"
const upscale4ModelID = "realesrgan_x4"
const upscale4WeightID = "RealESRGAN_x4plus.pth"
const depthModelID = "depth_anything_v2_small"
const depthWeightID = "depth_anything_v2_vits.pth"
const inpaintModelID = "big_lama"
const inpaintWeightID = "big-lama"
const generationModelID = "sd15_inpaint"
const generationWeightID = "stable-diffusion-v1-5/stable-diffusion-inpainting"

type styleModelReference struct {
	ID       string `json:"id"`
	WeightID string `json:"weight_id"`
}

type styleExecutionNeeds struct {
	Mode  string `json:"mode"`
	Local struct {
		Enabled bool `json:"enabled"`
	} `json:"local"`
	Models []styleModelReference `json:"models"`
	Beauty struct {
		Enabled bool `json:"enabled"`
	} `json:"beauty"`
	Defects struct {
		Enabled bool   `json:"enabled"`
		Policy  string `json:"policy"`
	} `json:"defects"`
	Geometry struct {
		Enabled bool `json:"enabled"`
	} `json:"geometry"`
	Extensions struct {
		Series struct {
			Enabled bool `json:"enabled"`
		} `json:"series"`
		Composition struct {
			Enabled          bool    `json:"enabled"`
			OutpaintPolicy   string  `json:"outpaint_policy"`
			OutpaintPrompt   string  `json:"outpaint_prompt"`
			OutpaintNegative string  `json:"outpaint_negative_prompt"`
			OutpaintGuidance float64 `json:"outpaint_guidance_scale"`
		} `json:"composition"`
		PortraitDetail struct {
			Enabled bool `json:"enabled"`
		} `json:"portrait_detail"`
		Restoration struct {
			Denoise    string `json:"denoise"`
			Upscale    string `json:"upscale"`
			SensorDust string `json:"sensor_dust"`
		} `json:"restoration"`
		Background struct {
			Mode         string    `json:"mode"`
			SolidRGB     []float64 `json:"solid_rgb"`
			BlurStrength float64   `json:"blur_strength"`
		} `json:"background"`
		Inpainting struct {
			Enabled    bool   `json:"enabled"`
			Model      string `json:"model"`
			Policy     string `json:"policy"`
			MaskSource string `json:"mask_source"`
		} `json:"inpainting"`
		Generation struct {
			Enabled           bool    `json:"enabled"`
			Model             string  `json:"model"`
			Prompt            string  `json:"prompt"`
			NegativePrompt    string  `json:"negative_prompt"`
			Steps             int     `json:"steps"`
			Guidance          float64 `json:"guidance_scale"`
			RequireAcceptance bool    `json:"require_acceptance"`
		} `json:"generation"`
	} `json:"extensions"`
}

func mergeOverrideNeeds(s *styleExecutionNeeds, raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var envelope struct {
		Modules map[string]json.RawMessage `json:"modules"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		return errors.New("invalid override execution requirements")
	}
	for name, value := range envelope.Modules {
		var target any
		switch name {
		case "beauty":
			target = &s.Beauty
		case "defects":
			target = &s.Defects
		case "geometry":
			target = &s.Geometry
		case "extensions":
			var ext map[string]json.RawMessage
			if json.Unmarshal(value, &ext) != nil {
				return errors.New("invalid override extensions")
			}
			for key, entry := range ext {
				switch key {
				case "series":
					target = &s.Extensions.Series
				case "composition":
					target = &s.Extensions.Composition
				case "portrait_detail":
					target = &s.Extensions.PortraitDetail
				case "restoration":
					target = &s.Extensions.Restoration
				case "background":
					target = &s.Extensions.Background
				case "inpainting":
					target = &s.Extensions.Inpainting
				case "generation":
					target = &s.Extensions.Generation
				default:
					continue
				}
				if json.Unmarshal(entry, target) != nil {
					return errors.New("invalid override extension")
				}
			}
			continue
		default:
			continue
		}
		if json.Unmarshal(value, target) != nil {
			return errors.New("invalid override module")
		}
	}
	return nil
}

func supportsApplyV4(s styleExecutionNeeds) bool {
	inpaintOK := !s.Extensions.Inpainting.Enabled || s.Extensions.Inpainting.Model == inpaintModelID && s.Extensions.Inpainting.Policy == "review" && s.Extensions.Inpainting.MaskSource == "per_image"
	g := s.Extensions.Generation
	generationOK := !g.Enabled || g.Model == generationModelID && len(g.Prompt) >= 1 && len(g.Prompt) <= 1000 && len(g.NegativePrompt) <= 1000 && g.Steps == 25 && g.Guidance >= 1 && g.Guidance <= 20 && g.RequireAcceptance
	c := s.Extensions.Composition
	compositionOK := !c.Enabled || c.OutpaintPolicy == "" || c.OutpaintPolicy == "off" || c.OutpaintPolicy == "review" && len(c.OutpaintPrompt) >= 1 && len(c.OutpaintPrompt) <= 1000 && len(c.OutpaintNegative) <= 1000 && c.OutpaintGuidance >= 1 && c.OutpaintGuidance <= 20
	return inpaintOK && generationOK && (!s.Defects.Enabled || s.Defects.Policy == "review") && !s.Extensions.PortraitDetail.Enabled &&
		compositionOK &&
		(s.Extensions.Restoration.Denoise == "" || s.Extensions.Restoration.Denoise == "off" || s.Extensions.Restoration.Denoise == denoiseModelID) &&
		(s.Extensions.Restoration.Upscale == "" || s.Extensions.Restoration.Upscale == "off" || s.Extensions.Restoration.Upscale == "2x" || s.Extensions.Restoration.Upscale == "4x") &&
		(s.Extensions.Restoration.SensorDust == "" || s.Extensions.Restoration.SensorDust == "off" || s.Extensions.Restoration.SensorDust == "review") &&
		(s.Extensions.Background.Mode == "" || s.Extensions.Background.Mode == "keep" || s.Extensions.Background.Mode == "transparent" || s.Extensions.Background.Mode == "solid" || s.Extensions.Background.Mode == "blur" || s.Extensions.Background.Mode == "generate")
}

func supportsApplyV4WithOverrides(s styleExecutionNeeds, raw json.RawMessage) bool {
	portrait := s.Extensions.PortraitDetail.Enabled
	creative := s.Extensions.Inpainting.Enabled || s.Extensions.Generation.Enabled || s.Extensions.Background.Mode == "generate"
	if !s.Extensions.PortraitDetail.Enabled && !creative {
		return supportsApplyV4(s)
	}
	if s.Extensions.PortraitDetail.Enabled && s.Geometry.Enabled || len(raw) == 0 {
		return false
	}
	var override struct {
		MaskRefs []struct {
			Purpose string `json:"purpose"`
		} `json:"mask_refs"`
	}
	if json.Unmarshal(raw, &override) != nil {
		return false
	}
	hasRegion, hasInpaint, hasGeneration := false, false, false
	for _, binding := range override.MaskRefs {
		if strings.HasPrefix(binding.Purpose, "portrait_") {
			hasRegion = true
		}
		hasInpaint = hasInpaint || binding.Purpose == "inpaint_target"
		hasGeneration = hasGeneration || binding.Purpose == "generation_target"
	}
	s.Extensions.PortraitDetail.Enabled = false
	return (!portrait || hasRegion) && (!s.Extensions.Inpainting.Enabled || hasInpaint) &&
		(!s.Extensions.Generation.Enabled && s.Extensions.Background.Mode != "generate" || hasGeneration) && supportsApplyV4(s)
}

func hasPersonModel(models []styleModelReference) bool {
	for _, model := range models {
		if model.ID == personModelID && model.WeightID == personWeightID {
			return true
		}
	}
	return false
}

func hasExactModel(models []styleModelReference, id, weight string) bool {
	for _, model := range models {
		if model.ID == id && model.WeightID == weight {
			return true
		}
	}
	return false
}

func validateJobOverride(raw json.RawMessage) error {
	var value struct {
		SchemaVersion int                        `json:"schema_version"`
		Modules       map[string]json.RawMessage `json:"modules"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&value) != nil || value.SchemaVersion != 1 || len(value.Modules) == 0 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	allowed := map[string]bool{"strength": true, "global": true, "local": true, "beauty": true, "defects": true, "geometry": true, "extensions": true}
	for name, body := range value.Modules {
		if !allowed[name] || len(body) == 0 || string(body) == "null" {
			return domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return nil
}

func normalizeApply(req ApplyRequest) (ApplyRequest, float64, error) {
	req.Scope = strings.ToLower(strings.TrimSpace(req.Scope))
	req.OutputName = strings.TrimSpace(req.OutputName)
	strength := 1.0
	if req.Strength != nil {
		strength = *req.Strength
	}
	if req.SourceRevision < 1 || math.IsNaN(strength) || math.IsInf(strength, 0) || strength < 0 || strength > 1 || len([]rune(req.OutputName)) > 120 || req.OverridesID != nil && *req.OverridesID == uuid.Nil || len(req.JobOverrides) > 1048576 {
		return req, 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	if len(req.JobOverrides) > 0 {
		body, err := canonical.Bytes(req.JobOverrides)
		if err != nil || len(body) == 0 || body[0] != '{' {
			return req, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		if err = validateJobOverride(body); err != nil {
			return req, 0, err
		}
		req.JobOverrides = body
	}
	switch req.Scope {
	case "all":
		if len(req.ItemIDs) > 0 || req.SelectionSnapshotID != nil {
			return req, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
	case "selected", "all_change_selected":
		if (len(req.ItemIDs) > 0) == (req.SelectionSnapshotID != nil) {
			return req, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		if len(req.ItemIDs) > 5000 {
			return req, 0, domain.NewError("VALIDATION_FAILED", 422)
		}
		if len(req.ItemIDs) > 0 {
			sort.Slice(req.ItemIDs, func(i, j int) bool { return bytes.Compare(req.ItemIDs[i][:], req.ItemIDs[j][:]) < 0 })
			for i, id := range req.ItemIDs {
				if id == uuid.Nil || i > 0 && id == req.ItemIDs[i-1] {
					return req, 0, domain.NewError("VALIDATION_FAILED", 422)
				}
			}
		}
	default:
		return req, 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	v := strength
	req.Strength = &v
	return req, strength, nil
}

func loadApplySelection(ctx context.Context, tx pgx.Tx, tid, collectionID uuid.UUID, ids []uuid.UUID) ([]applySelection, error) {
	var selected any
	if len(ids) > 0 {
		selected = ids
	}
	rows, err := tx.Query(ctx, `SELECT i.id,i.asset_id,i.asset_version_id,v.master_artifact_id,v.content_hash,i.position,orig.bytes
	 FROM photo.collection_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.asset_id=i.asset_id
	 JOIN photo.assets a ON a.tenant_id=i.tenant_id AND a.id=i.asset_id AND a.project_id=(SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2) AND a.deleted_at IS NULL AND a.upload_state='verified'
	 JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id AND orig.state='ready'
	 JOIN photo.artifacts ar ON ar.tenant_id=v.tenant_id AND ar.id=v.master_artifact_id AND ar.sha256=v.content_hash AND ar.state='ready' AND ar.object_version IS NOT NULL AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp())
	 WHERE i.tenant_id=$1 AND i.collection_id=$2 AND ($3::uuid[] IS NULL OR i.id=ANY($3::uuid[])) ORDER BY i.position,i.id LIMIT 5001`, tid, collectionID, selected)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []applySelection
	for rows.Next() {
		var v applySelection
		if err = rows.Scan(&v.ItemID, &v.AssetID, &v.AssetVersionID, &v.ArtifactID, &v.SHA256, &v.Position, &v.OriginalBytes); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(out) < 1 || len(out) > 5000 {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if len(ids) > 0 && len(out) != len(ids) {
		return nil, domain.NewError("SNAPSHOT_STALE", 409)
	}
	return out, nil
}

func (s Service) Apply(ctx context.Context, who auth.Identity, tid, sourceID uuid.UUID, key string, raw ApplyRequest) (idempotency.Response, error) {
	if raw.StyleVersionID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return s.apply(ctx, who, tid, sourceID, key, raw, false, "Run.Apply", "", nil, nil)
}

// ApplyWithCommit admits a child Apply and lets its parent workflow commit its
// decision in the same transaction. The hook must only make database changes
// through tx; an error rolls back the child snapshot, run, quota and output.
// It is not invoked on an idempotent replay, because the original transaction
// has already committed the parent decision. decisionSHA must bind the entire
// canonical parent decision, including the approved group set and recipe ref.
func (s Service) ApplyWithCommit(ctx context.Context, who auth.Identity,
	tid, sourceID uuid.UUID, key string, raw ApplyRequest, decisionSHA string,
	itemOverrides []BoundItemOverride,
	commit func(context.Context, pgx.Tx, ApplyAccepted) error) (idempotency.Response, error) {
	if raw.StyleVersionID == uuid.Nil || commit == nil || len(itemOverrides) == 0 ||
		raw.Scope != "selected" || len(raw.ItemIDs) == 0 ||
		len(itemOverrides) != len(raw.ItemIDs) || len(itemOverrides) > 5000 ||
		raw.SelectionSnapshotID != nil ||
		raw.OverridesID != nil || len(raw.JobOverrides) != 0 ||
		len(decisionSHA) != 64 || !isLowerHexSHA(decisionSHA) {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	bytesTotal := 0
	for _, item := range itemOverrides {
		bytesTotal += len(item.Body)
		if len(item.Body) < 2 || len(item.Body) > 1<<20 || bytesTotal > 8<<20 {
			return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	return s.apply(ctx, who, tid, sourceID, key, raw, false, "SessionAnalysis.Apply", decisionSHA, itemOverrides, commit)
}

func (s Service) apply(ctx context.Context, who auth.Identity, tid, sourceID uuid.UUID, key string, raw ApplyRequest, identity bool, route, decisionSHA string,
	itemOverrides []BoundItemOverride,
	commit func(context.Context, pgx.Tx, ApplyAccepted) error) (idempotency.Response, error) {
	var out idempotency.Response
	req, strength, err := normalizeApply(raw)
	if err != nil {
		return out, err
	}
	if sourceID == uuid.Nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err = tenant.WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var projectID uuid.UUID
		e := tx.QueryRow(ctx, `SELECT project_id FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND deleted_at IS NULL`, tid, sourceID).Scan(&projectID)
		if errors.Is(e, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if e != nil {
			return e
		}
		if _, e = project.Authorize(ctx, tx, tid, projectID, who.UserID, role, domain.Editor, false); e != nil {
			return e
		}
		fingerprint := any(struct {
			Source  uuid.UUID    `json:"source_collection_id"`
			Request ApplyRequest `json:"request"`
		}{sourceID, req})
		if decisionSHA != "" {
			fingerprint = struct {
				Source        uuid.UUID           `json:"source_collection_id"`
				Request       ApplyRequest        `json:"request"`
				DecisionSHA   string              `json:"decision_sha"`
				ItemOverrides []BoundItemOverride `json:"item_overrides"`
			}{sourceID, req, decisionSHA, itemOverrides}
		}
		out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: route, Key: key}, fingerprint, time.Now().Add(30*24*time.Hour), func() (idempotency.Response, error) {
			var sourceName, state string
			var revision int64
			e := tx.QueryRow(ctx, `SELECT name,state,revision FROM photo.collections WHERE tenant_id=$1 AND id=$2 AND project_id=$3 AND deleted_at IS NULL FOR UPDATE`, tid, sourceID, projectID).Scan(&sourceName, &state, &revision)
			if e != nil {
				return idempotency.Response{}, e
			}
			if state == "archived" || revision != req.SourceRevision {
				return idempotency.Response{}, domain.NewError("REVISION_CONFLICT", 409)
			}
			var styleID, styleArtifactID uuid.UUID
			var styleConfig json.RawMessage
			var styleHash, approval string
			if identity {
				styleConfig, e = canonical.Bytes(identityStyleJSON)
				if e == nil {
					styleHash, e = canonical.Hash(json.RawMessage(styleConfig))
				}
				approval = "accepted"
			} else {
				e = tx.QueryRow(ctx, `SELECT v.style_id,v.config_json,v.config_hash,v.approval_status,a.id FROM photo.style_versions v JOIN photo.styles st ON st.tenant_id=v.tenant_id AND st.id=v.style_id JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.export_artifact_id AND a.sha256=v.config_hash AND a.state='ready' AND a.object_version IS NOT NULL AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()) WHERE v.tenant_id=$1 AND v.id=$2 AND st.project_id=$3 AND st.archived_at IS NULL`, tid, req.StyleVersionID, projectID).Scan(&styleID, &styleConfig, &styleHash, &approval, &styleArtifactID)
			}
			if errors.Is(e, pgx.ErrNoRows) {
				return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
			}
			if e != nil {
				return idempotency.Response{}, e
			}
			actual, e := canonical.Hash(json.RawMessage(styleConfig))
			if e != nil || actual != styleHash {
				return idempotency.Response{}, errors.New("stored style hash mismatch")
			}
			if approval != "accepted" {
				return idempotency.Response{}, domain.NewError("STYLE_NOT_ACCEPTED", 409)
			}
			var styleNeeds styleExecutionNeeds
			if json.Unmarshal(styleConfig, &styleNeeds) != nil || styleNeeds.Mode != "fixed" && styleNeeds.Mode != "adaptive" {
				return idempotency.Response{}, errors.New("invalid stored style execution requirements")
			}
			var overrideConfig json.RawMessage
			var overrideHash *string
			var overrideVersion uuid.UUID
			var overrideInputSHA string
			if req.OverridesID != nil {
				var h string
				e = tx.QueryRow(ctx, `SELECT config,config_hash,asset_version_id,input_sha256 FROM photo.overrides WHERE tenant_id=$1 AND id=$2 AND created_by=$3`, tid, *req.OverridesID, who.UserID).Scan(&overrideConfig, &h, &overrideVersion, &overrideInputSHA)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
				computed, e := canonical.Hash(json.RawMessage(overrideConfig))
				if e != nil || computed != h {
					return idempotency.Response{}, errors.New("stored overrides hash mismatch")
				}
				overrideHash = &h
			}
			if e = mergeOverrideNeeds(&styleNeeds, overrideConfig); e != nil {
				return idempotency.Response{}, e
			}
			var graphBeauty json.RawMessage
			if decisionSHA != "" {
				claimed := make([]applySelection, len(itemOverrides))
				for i, item := range itemOverrides {
					claimed[i] = applySelection{ItemID: item.ItemID,
						AssetVersionID: item.AssetVersionID, SHA256: item.InputSHA256}
				}
				_, graphBeauty, e = validateBoundItemOverrides(claimed, itemOverrides)
				if e != nil {
					return idempotency.Response{}, e
				}
				if len(graphBeauty) > 0 {
					styleNeeds.Beauty.Enabled = true
				}
			}
			seriesRequested := styleNeeds.Extensions.Series.Enabled
			geometryRequested := styleNeeds.Geometry.Enabled
			faceRequired := styleNeeds.Beauty.Enabled || styleNeeds.Defects.Enabled
			backgroundRequested := styleNeeds.Extensions.Background.Mode == "transparent" || styleNeeds.Extensions.Background.Mode == "solid" || styleNeeds.Extensions.Background.Mode == "blur" || styleNeeds.Extensions.Background.Mode == "generate"
			backgroundBlur := styleNeeds.Extensions.Background.Mode == "blur"
			personRequired := styleNeeds.Mode == "adaptive" || styleNeeds.Local.Enabled || seriesRequested || geometryRequested || faceRequired || backgroundRequested || styleNeeds.Extensions.Composition.Enabled
			personBackend, personRuntimeLock := "", ""
			if personRequired {
				if !identity && !hasPersonModel(styleNeeds.Models) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && geometryRequested && !hasExactModel(styleNeeds.Models, poseModelID, poseWeightID) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && faceRequired && (!hasExactModel(styleNeeds.Models, faceDetectModelID, faceDetectWeightID) ||
					!hasExactModel(styleNeeds.Models, faceLandmarkModelID, faceLandmarkWeightID) ||
					!hasExactModel(styleNeeds.Models, faceParseModelID, faceParseWeightID)) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				if !identity && backgroundBlur && !hasExactModel(styleNeeds.Models, depthModelID, depthWeightID) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `
					SELECT w.lock_id, candidate.backend
					FROM photo.worker_registrations w
					CROSS JOIN LATERAL (
						SELECT backend
						FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY AS preferred(backend, priority)
						WHERE w.backends ? backend
						  AND (w.qualified_operations->'mask.person') ? backend
						  AND (NOT $2 OR (w.qualified_operations->'pose.detect') ? backend)
						  AND (NOT $3 OR ((w.qualified_operations->'face.detect') ? backend
						                    AND (w.qualified_operations->'face.landmarks') ? backend
						                    AND (w.qualified_operations->'face.parse') ? backend))
						  AND (NOT $4 OR (w.qualified_operations->'depth.predict') ? backend)
						ORDER BY priority
						LIMIT 1
					) candidate
					WHERE w.tenant_id=$1
					  AND w.enabled
					  AND w.last_seen>clock_timestamp()-interval '30 seconds'
					  AND w.resource_classes ? 'gpu'
					  AND w.lock_id ~ '^[0-9a-f]{64}$'
					ORDER BY CASE candidate.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END, w.worker_id
					LIMIT 1`, tid, geometryRequested, faceRequired, backgroundBlur).Scan(&personRuntimeLock, &personBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("PERSON_MASK_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
			}
			restorationDenoise := styleNeeds.Extensions.Restoration.Denoise == denoiseModelID
			restorationUpscale := styleNeeds.Extensions.Restoration.Upscale == "2x" || styleNeeds.Extensions.Restoration.Upscale == "4x"
			restorationBackend, restorationRuntimeLock := "", ""
			if restorationDenoise || restorationUpscale {
				if !identity && restorationDenoise && !hasExactModel(styleNeeds.Models, denoiseModelID, denoiseWeightID) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				upscaleID, upscaleWeight := upscale2ModelID, upscale2WeightID
				if styleNeeds.Extensions.Restoration.Upscale == "4x" {
					upscaleID, upscaleWeight = upscale4ModelID, upscale4WeightID
				}
				if !identity && restorationUpscale && !hasExactModel(styleNeeds.Models, upscaleID, upscaleWeight) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `SELECT w.lock_id,c.backend FROM photo.worker_registrations w CROSS JOIN LATERAL (
				 SELECT backend FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY p(backend,priority)
				 WHERE w.backends ? backend AND (NOT $2 OR (w.qualified_operations->'image.denoise') ? backend)
				  AND (NOT $3 OR (w.qualified_operations->'image.upscale') ? backend) ORDER BY priority LIMIT 1) c
				 WHERE w.tenant_id=$1 AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds' AND w.resource_classes ? 'gpu'
				  AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY CASE c.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END,w.worker_id LIMIT 1`, tid, restorationDenoise, restorationUpscale).Scan(&restorationRuntimeLock, &restorationBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("RESTORATION_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
			}
			creativeInpaint := styleNeeds.Extensions.Inpainting.Enabled
			creativeGeneration := styleNeeds.Extensions.Generation.Enabled || styleNeeds.Extensions.Background.Mode == "generate" || styleNeeds.Extensions.Composition.Enabled && styleNeeds.Extensions.Composition.OutpaintPolicy == "review"
			creativeBackend, creativeRuntimeLock := "", ""
			if creativeInpaint || creativeGeneration {
				if !identity && (creativeInpaint && !hasExactModel(styleNeeds.Models, inpaintModelID, inpaintWeightID) || creativeGeneration && !hasExactModel(styleNeeds.Models, generationModelID, generationWeightID)) {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				e = tx.QueryRow(ctx, `SELECT w.lock_id,c.backend FROM photo.worker_registrations w CROSS JOIN LATERAL (
				 SELECT backend FROM unnest(ARRAY['cuda','mps','cpu']) WITH ORDINALITY p(backend,priority)
					 WHERE w.backends ? backend AND (NOT $2 OR (w.qualified_operations->'image.inpaint') ? backend)
				  AND (NOT $3 OR ((w.qualified_operations->'image.generate') ? backend OR (w.qualified_operations->'image.outpaint') ? backend))
				  AND (NOT $4 OR (w.qualified_operations->'image.outpaint') ? backend) ORDER BY priority LIMIT 1) c
				 WHERE w.tenant_id=$1 AND w.enabled AND w.last_seen>clock_timestamp()-interval '30 seconds' AND w.resource_classes ? 'gpu'
				  AND w.lock_id ~ '^[0-9a-f]{64}$' ORDER BY CASE c.backend WHEN 'cuda' THEN 0 WHEN 'mps' THEN 1 ELSE 2 END,w.worker_id LIMIT 1`, tid, creativeInpaint, styleNeeds.Extensions.Generation.Enabled || styleNeeds.Extensions.Background.Mode == "generate", styleNeeds.Extensions.Composition.Enabled && styleNeeds.Extensions.Composition.OutpaintPolicy == "review").Scan(&creativeRuntimeLock, &creativeBackend)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("CREATIVE_CAPABILITY_UNAVAILABLE", 409)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
			}
			var snapshotID uuid.UUID
			var snapshotDigest string
			var items []applySelection
			var expectedSnapshotCount int64
			if req.SelectionSnapshotID != nil {
				snapshotID = *req.SelectionSnapshotID
				e = tx.QueryRow(ctx, `SELECT digest,item_count FROM photo.snapshots WHERE tenant_id=$1 AND id=$2 AND kind='selection' AND collection_id=$3 AND source_revision=$4 AND principal_type='user' AND principal_id=$5 AND expires_at>clock_timestamp()`, tid, snapshotID, sourceID, revision, who.UserID).Scan(&snapshotDigest, &expectedSnapshotCount)
				if errors.Is(e, pgx.ErrNoRows) {
					return idempotency.Response{}, domain.NewError("SNAPSHOT_STALE", 409)
				}
				if e != nil {
					return idempotency.Response{}, e
				}
				if expectedSnapshotCount < 1 || expectedSnapshotCount > 5000 {
					return idempotency.Response{}, domain.NewError("SNAPSHOT_STALE", 409)
				}
			} else {
				ids := req.ItemIDs
				if req.Scope == "all" {
					ids = nil
				}
				items, e = loadApplySelection(ctx, tx, tid, sourceID, ids)
				if e != nil {
					return idempotency.Response{}, e
				}
				snapshotID = uuid.New()
				snapshotDigest, e = canonical.Hash(items)
				if e != nil {
					return idempotency.Response{}, e
				}
				expires := time.Now().Add(30 * 24 * time.Hour)
				if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshots(id,tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count,expires_at) VALUES($1,$2,'selection',$3,$4,'user',$5,$6,$7,$8)`, snapshotID, tid, sourceID, revision, who.UserID, snapshotDigest, len(items), expires); e != nil {
					return idempotency.Response{}, e
				}
				for i, v := range items {
					payload, _ := canonical.Marshal(map[string]any{"position": v.Position, "asset_id": v.AssetID})
					if _, e = tx.Exec(ctx, `INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload) VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, tid, snapshotID, i, v.ItemID, v.AssetVersionID, v.ArtifactID, v.SHA256, payload); e != nil {
						return idempotency.Response{}, e
					}
				}
			}
			if len(items) == 0 {
				rows, e := tx.Query(ctx, `SELECT i.item_id,a.asset_id,i.asset_version_id,i.artifact_id,i.sha256,(i.payload->>'position')::bigint,orig.bytes FROM photo.snapshot_items i JOIN photo.asset_versions v ON v.tenant_id=i.tenant_id AND v.id=i.asset_version_id AND v.master_artifact_id=i.artifact_id AND v.content_hash=i.sha256 JOIN photo.assets a ON a.tenant_id=v.tenant_id AND a.id=v.asset_id AND a.project_id=$3 AND a.deleted_at IS NULL AND a.upload_state='verified' JOIN photo.artifacts orig ON orig.tenant_id=a.tenant_id AND orig.id=a.original_artifact_id AND orig.state='ready' JOIN photo.artifacts ar ON ar.tenant_id=i.tenant_id AND ar.id=i.artifact_id AND ar.sha256=i.sha256 AND ar.state='ready' AND ar.object_version IS NOT NULL AND (ar.expires_at IS NULL OR ar.expires_at>clock_timestamp()) WHERE i.tenant_id=$1 AND i.snapshot_id=$2 ORDER BY i.ordinal`, tid, snapshotID, projectID)
				if e != nil {
					return idempotency.Response{}, e
				}
				defer rows.Close()
				for rows.Next() {
					var v applySelection
					if e = rows.Scan(&v.ItemID, &v.AssetID, &v.AssetVersionID, &v.ArtifactID, &v.SHA256, &v.Position, &v.OriginalBytes); e != nil {
						return idempotency.Response{}, e
					}
					items = append(items, v)
				}
				if e = rows.Err(); e != nil {
					return idempotency.Response{}, e
				}
				if len(items) < 1 || expectedSnapshotCount > 0 && int64(len(items)) != expectedSnapshotCount {
					return idempotency.Response{}, domain.NewError("SNAPSHOT_STALE", 409)
				}
			}
			if req.OverridesID != nil && (len(items) != 1 || items[0].AssetVersionID != overrideVersion || items[0].SHA256 != overrideInputSHA) {
				return idempotency.Response{}, domain.NewError("OVERRIDE_SOURCE_MISMATCH", 409)
			}
			var boundOverrides []BoundItemOverride
			if decisionSHA != "" {
				boundOverrides, _, e = validateBoundItemOverrides(items, itemOverrides)
				if e != nil {
					return idempotency.Response{}, e
				}
			}
			runID, outputID := uuid.New(), uuid.New()
			name := req.OutputName
			if name == "" {
				name = sourceName + " — processed"
				if len([]rune(name)) > 120 {
					name = string([]rune(name)[:120])
				}
			}
			const maxSafe = int64(9007199254740991)
			var estimatedBytes int64
			for _, item := range items {
				if item.OriginalBytes < 1 || item.OriginalBytes > (maxSafe-estimatedBytes)/2 {
					return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
				}
				estimatedBytes += item.OriginalBytes * 2
			}
			manifestAllowance := int64(len(items)) * 1048576
			if estimatedBytes > maxSafe-manifestAllowance {
				return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
			}
			estimatedBytes += manifestAllowance
			var storage, reserved, byteLimit int64
			e = tx.QueryRow(ctx, `SELECT u.storage_bytes,u.reserved_bytes,(t.quotas->>'storage_bytes')::bigint FROM photo.tenant_usage u JOIN photo.tenants t ON t.id=u.tenant_id WHERE u.tenant_id=$1 FOR UPDATE OF u`, tid).Scan(&storage, &reserved, &byteLimit)
			if e != nil {
				return idempotency.Response{}, e
			}
			if byteLimit < estimatedBytes || storage > byteLimit-estimatedBytes || reserved > byteLimit-estimatedBytes-storage {
				return idempotency.Response{}, domain.NewError("QUOTA_EXCEEDED", 422)
			}
			var styleVersion *uuid.UUID
			var styleArtifact *uuid.UUID
			if !identity {
				styleVersion, styleArtifact = &req.StyleVersionID, &styleArtifactID
			}
			seriesEnabled := seriesRequested && len(items) >= 2
			var seriesPins seriesApplyQualification
			if seriesEnabled {
				e = tx.QueryRow(ctx, `SELECT model_lock_sha,runtime_lock_sha,
				 device_profile_sha FROM photo.v23_series_apply_qualification($1,$2)`,
					tid, projectID).Scan(&seriesPins.ModelLockSHA,
					&seriesPins.RuntimeLockSHA, &seriesPins.DeviceProfileSHA)
				if errors.Is(e, pgx.ErrNoRows) {
					seriesPins = seriesApplyQualification{}
				} else if e != nil {
					return idempotency.Response{}, e
				} else {
					for _, pin := range []string{seriesPins.ModelLockSHA,
						seriesPins.RuntimeLockSHA, seriesPins.DeviceProfileSHA} {
						if _, e = domain.ParseDigest(pin); e != nil {
							return idempotency.Response{}, e
						}
					}
				}
			}
			pipelineVersion, graphVersion := 3, 3
			if identity || supportsApplyV4WithOverrides(styleNeeds, overrideConfig) {
				pipelineVersion, graphVersion = 4, 4
			}
			var graphStyleConfig json.RawMessage
			if decisionSHA != "" {
				if graphVersion != 4 {
					return idempotency.Response{}, domain.NewError("STYLE_MODEL_BINDING_MISSING", 409)
				}
				graphStyleConfig, e = analysisGraphStyle(styleConfig, graphBeauty)
				if e != nil {
					return idempotency.Response{}, e
				}
			}
			config := struct {
				Schema              string              `json:"schema_version"`
				Pipeline            string              `json:"pipeline"`
				PipelineVersion     int                 `json:"pipeline_version"`
				Renderer            string              `json:"renderer_version"`
				Source              uuid.UUID           `json:"source_collection_id"`
				SourceRevision      int64               `json:"source_revision"`
				Input               uuid.UUID           `json:"input_snapshot_id"`
				InputDigest         string              `json:"input_snapshot_digest"`
				StyleVersion        *uuid.UUID          `json:"style_version_id,omitempty"`
				StyleArtifact       *uuid.UUID          `json:"style_artifact_id,omitempty"`
				StyleHash           string              `json:"style_hash"`
				StyleConfig         json.RawMessage     `json:"style_config"`
				GraphStyleConfig    json.RawMessage     `json:"graph_style_config,omitempty"`
				Identity            bool                `json:"identity_style"`
				PersonRequired      bool                `json:"person_mask_required"`
				PersonBackend       string              `json:"person_backend,omitempty"`
				PersonLock          string              `json:"person_runtime_lock_id,omitempty"`
				SeriesEnabled       bool                `json:"series_enabled"`
				SeriesModelLock     string              `json:"series_model_lock_sha,omitempty"`
				SeriesRuntimeLock   string              `json:"series_runtime_lock_sha,omitempty"`
				SeriesDeviceProfile string              `json:"series_device_profile_sha,omitempty"`
				GeometryEnabled     bool                `json:"geometry_enabled"`
				BeautyEnabled       bool                `json:"beauty_enabled"`
				PortraitDetail      bool                `json:"portrait_detail_enabled"`
				DefectsEnabled      bool                `json:"defects_enabled"`
				RestorationBackend  string              `json:"restoration_backend,omitempty"`
				RestorationLock     string              `json:"restoration_runtime_lock_id,omitempty"`
				CreativeBackend     string              `json:"creative_backend,omitempty"`
				CreativeLock        string              `json:"creative_runtime_lock_id,omitempty"`
				Strength            float64             `json:"strength"`
				Scope               string              `json:"scope"`
				OverridesID         *uuid.UUID          `json:"overrides_id,omitempty"`
				OverridesHash       *string             `json:"overrides_hash,omitempty"`
				OverrideConfig      json.RawMessage     `json:"override_config,omitempty"`
				JobOverrides        json.RawMessage     `json:"job_overrides,omitempty"`
				AnalysisDecisionSHA string              `json:"analysis_decision_sha,omitempty"`
				ItemOverrides       []BoundItemOverride `json:"item_overrides,omitempty"`
				FailurePolicy       string              `json:"failure_policy"`
				Seed                uint32              `json:"seed"`
			}{"1", "apply", pipelineVersion, "photo_renderer_v2", sourceID, revision, snapshotID, snapshotDigest, styleVersion, styleArtifact, styleHash, styleConfig, graphStyleConfig, identity, personRequired, personBackend, personRuntimeLock, seriesEnabled, seriesPins.ModelLockSHA, seriesPins.RuntimeLockSHA, seriesPins.DeviceProfileSHA, styleNeeds.Geometry.Enabled, styleNeeds.Beauty.Enabled, styleNeeds.Extensions.PortraitDetail.Enabled, styleNeeds.Defects.Enabled, restorationBackend, restorationRuntimeLock, creativeBackend, creativeRuntimeLock, strength, req.Scope, req.OverridesID, overrideHash, overrideConfig, req.JobOverrides, decisionSHA, boundOverrides, "continue_items", 42}
			configBytes, e := canonical.Marshal(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			configHash, e := canonical.Hash(config)
			if e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.collections(id,tenant_id,project_id,name,state,source_collection_id,source_revision,source_run_id,applied_style_version_id) VALUES($1,$2,$3,$4,'draft',$5,$6,$7,$8)`, outputID, tid, projectID, name, sourceID, revision, runID, styleVersion); e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,output_collection_id,style_version_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES($1,$2,'apply','queued',$3,$4,$5,$6,$7,$8,'continue_items',false)`, runID, tid, snapshotID, outputID, styleVersion, configBytes, configHash, who.UserID); e != nil {
				return idempotency.Response{}, e
			}
			reservationID := uuid.New()
			if _, e = tx.Exec(ctx, `INSERT INTO photo.quota_reservations(id,tenant_id,principal_type,principal_id,holder_type,holder_id,bytes,files,state,expires_at) VALUES($1,$2,'user',$3,'run',$4,$5,0,'reserved',$6)`, reservationID, tid, who.UserID, runID, estimatedBytes, time.Now().Add(30*24*time.Hour)); e != nil {
				return idempotency.Response{}, e
			}
			if _, e = tx.Exec(ctx, `UPDATE photo.tenant_usage SET reserved_bytes=reserved_bytes+$2 WHERE tenant_id=$1`, tid, estimatedBytes); e != nil {
				return idempotency.Response{}, e
			}
			initialPhase := "resolve"
			if personRequired {
				initialPhase = "mask"
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.apply_builds(tenant_id,run_id,source_collection_id,style_version_id,item_count,phase,graph_version) VALUES($1,$2,$3,$4,$5,$6,$7)`, tid, runID, sourceID, styleVersion, len(items), initialPhase, graphVersion); e != nil {
				return idempotency.Response{}, e
			}
			if req.Scope == "all_change_selected" {
				if _, e = tx.Exec(ctx, `INSERT INTO photo.collection_items(tenant_id,collection_id,asset_id,asset_version_id,section_id,position,visibility,moderation_state,rating,flag,tags,created_by_run_id,source_item_id) SELECT i.tenant_id,$3,i.asset_id,i.asset_version_id,NULL,i.position,'hidden','pending_review',i.rating,i.flag,i.tags,$4,i.id FROM photo.collection_items i WHERE i.tenant_id=$1 AND i.collection_id=$2 AND NOT EXISTS(SELECT FROM photo.snapshot_items s WHERE s.tenant_id=i.tenant_id AND s.snapshot_id=$5 AND s.item_id=i.id)`, tid, sourceID, outputID, runID, snapshotID); e != nil {
					return idempotency.Response{}, e
				}
			}
			if _, e = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'run.apply.create','run',$3,$4,jsonb_build_object('source_collection_id',$5::uuid,'output_collection_id',$6::uuid))`, tid, who.UserID, runID, uuid.New(), sourceID, outputID); e != nil {
				return idempotency.Response{}, e
			}
			accepted := ApplyAccepted{runID, outputID, "queued"}
			if commit != nil {
				if e = commit(ctx, tx, accepted); e != nil {
					return idempotency.Response{}, e
				}
			}
			body, e := canonical.Marshal(accepted)
			return idempotency.Response{Status: 202, Body: body, ResourceID: &runID}, e
		})
		return e
	})
	return out, err
}

func (s Service) Create(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, req CreateRequest) (idempotency.Response, error) {
	if req.Kind != "apply" || req.SourceCollectionID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	return s.Apply(ctx, who, tid, req.SourceCollectionID, key, req.ApplyRequest)
}

// SaveEdits enters the exact same immutable snapshot and rendering path as Apply.
func (s Service) SaveEdits(ctx context.Context, who auth.Identity, tid, sourceID uuid.UUID, key string, req SaveEditsRequest) (idempotency.Response, error) {
	if req.OverridesID == uuid.Nil || req.StyleVersionID != nil && *req.StyleVersionID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "selected" && len(req.SelectedItemIDs) == 0 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	style := uuid.Nil
	if req.StyleVersionID != nil {
		style = *req.StyleVersionID
	}
	return s.apply(ctx, who, tid, sourceID, key, ApplyRequest{
		SourceRevision: req.SourceRevision,
		StyleVersionID: style,
		Scope:          scope,
		ItemIDs:        req.SelectedItemIDs,
		OverridesID:    &req.OverridesID,
		OutputName:     req.OutputName,
	}, req.StyleVersionID == nil, "Run.SaveEdits", "", nil, nil)
}
