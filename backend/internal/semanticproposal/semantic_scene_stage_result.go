package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"math"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

type semanticSceneResultObject struct {
	Ref       v2input.Ref `json:"ref"`
	Key       string      `json:"key"`
	Version   string      `json:"version"`
	Bytes     int64       `json:"bytes"`
	MediaType string      `json:"media_type"`
}
type semanticSceneResultCatalog struct {
	Run       uuid.UUID                   `json:"run_id"`
	Task      uuid.UUID                   `json:"task_id"`
	Result    uuid.UUID                   `json:"result_id"`
	Attempt   int                         `json:"attempt"`
	Fence     int64                       `json:"fence"`
	ConfigSHA string                      `json:"config_sha"`
	Params    v2input.Ref                 `json:"params"`
	Root      v2input.Ref                 `json:"root"`
	Pixels    v2input.Ref                 `json:"pixels"`
	Invalid   *v2input.Ref                `json:"invalid"`
	Objects   []semanticSceneResultObject `json:"objects"`
}
type semanticSceneStageResult struct {
	planSeal string
	catalog  string
	rootBody string
	value    semanticSceneResultCatalog
}

func (s SQLTransferSourceReader) sceneStageResultCatalog(ctx context.Context, who auth.Identity, plan semanticSceneStagePlan) (semanticSceneResultCatalog, string, error) {
	var out semanticSceneResultCatalog
	if err := verifySemanticSceneStagePlan(ctx, plan); err != nil {
		return out, "", err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return out, "", v2input.ErrInvalid
	}
	var raw []byte
	err := tenant.WithTenant(ctx, s.DB, who, plan.shell.Tenant, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		return tx.QueryRow(ctx, "SELECT photo.semantic_scene_completed_result($1,$2,$3,$4,$5,$6)", plan.shell.Tenant, s.ProjectID, who.UserID, who.SessionID, plan.shell.Run, plan.shell.Tasks[0].ID).Scan(&raw)
	})
	if err != nil {
		return out, "", err
	}
	var fields map[string]json.RawMessage
	if len(raw) < 2 || len(raw) > 65536 || json.Unmarshal(raw, &fields) != nil || len(fields) != 11 {
		return out, "", v2input.ErrInvalid
	}
	for _, key := range []string{"run_id", "task_id", "result_id", "attempt", "fence", "config_sha", "params", "root", "pixels", "invalid", "objects"} {
		if len(fields[key]) == 0 || (key != "invalid" && string(fields[key]) == "null") {
			return out, "", v2input.ErrInvalid
		}
	}
	if json.Unmarshal(raw, &out) != nil || out.Run != plan.shell.Run || out.Task != plan.shell.Tasks[0].ID || out.Result == uuid.Nil || out.Attempt < 1 || out.Fence < 1 || out.ConfigSHA != plan.shell.ConfigSHA || out.Params != plan.prepared.published.ref {
		return semanticSceneResultCatalog{}, "", v2input.ErrConflict
	}
	// Exact immutable object descriptors are part of the proof, including every
	// binary consumed later. Never select the first output with a matching role.
	want := map[v2input.Ref]string{out.Params: "application/json", out.Root: "application/json", out.Pixels: "binary"}
	if out.Invalid != nil {
		want[*out.Invalid] = "binary"
	}
	if len(want) != 3+boolInt(out.Invalid != nil) || len(out.Objects) != len(want) {
		return semanticSceneResultCatalog{}, "", v2input.ErrInvalid
	}
	seen := map[v2input.Ref]bool{}
	seenIDs := map[uuid.UUID]bool{}
	for _, entry := range out.Objects {
		kind, exists := want[entry.Ref]
		if !exists || seen[entry.Ref] || seenIDs[entry.Ref.ArtifactID] || entry.Ref.ArtifactID == uuid.Nil || entry.Key == "" || entry.Version == "" || entry.Version == "null" || entry.Bytes < 1 || entry.Bytes > 9007199254740991 {
			return semanticSceneResultCatalog{}, "", v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(entry.Ref.SHA256); err != nil {
			return semanticSceneResultCatalog{}, "", err
		}
		if (kind == "application/json" && (entry.MediaType != kind || entry.Bytes > 8<<20)) || (kind == "binary" && entry.MediaType != "application/octet-stream" && entry.MediaType != "application/x-npy") {
			return semanticSceneResultCatalog{}, "", v2input.ErrInvalid
		}
		seen[entry.Ref] = true
		seenIDs[entry.Ref.ArtifactID] = true
	}
	return out, string(raw), ctx.Err()
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (s SQLTransferSourceReader) readSemanticSceneStageResult(ctx context.Context, who auth.Identity, plan semanticSceneStagePlan) (semanticSceneStageResult, error) {
	var zero semanticSceneStageResult
	before, pin, err := s.sceneStageResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	documents := map[v2input.Ref]json.RawMessage{}
	for _, entry := range before.Objects {
		if entry.MediaType != "application/json" {
			continue
		}
		object := objectstore.Object{Key: entry.Key, VersionID: entry.Version, Bytes: entry.Bytes, SHA256: entry.Ref.SHA256, ContentType: entry.MediaType}
		body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, entry.Ref)
		if err != nil {
			return zero, err
		}
		documents[entry.Ref] = body
	}
	if string(documents[before.Params]) != plan.prepared.published.body {
		return zero, v2input.ErrConflict
	}
	if err := validateSemanticSceneResultDocument(plan, before, documents[before.Root]); err != nil {
		return zero, err
	}
	_, after, err := s.sceneStageResultCatalog(ctx, who, plan)
	if err != nil {
		return zero, err
	}
	if pin != after {
		return zero, v2input.ErrConflict
	}
	return semanticSceneStageResult{planSeal: plan.seal, catalog: pin, rootBody: string(documents[before.Root]), value: before}, ctx.Err()
}

func validateSemanticSceneResultDocument(plan semanticSceneStagePlan, result semanticSceneResultCatalog, raw json.RawMessage) error {
	var source, root map[string]json.RawMessage
	var expected struct {
		Backend string `json:"Backend"`
	}
	if json.Unmarshal([]byte(plan.prepared.published.qualification.expected), &expected) != nil || (expected.Backend != "cuda" && expected.Backend != "mps") {
		return v2input.ErrInvalid
	}
	if json.Unmarshal([]byte(plan.prepared.published.anchor.sourceBody), &source) != nil || json.Unmarshal(raw, &root) != nil {
		return v2input.ErrInvalid
	}
	equal := func(a, b json.RawMessage) bool {
		var left, right any
		if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
			return false
		}
		// Geometry documents were physically SHA verified; compare their canonical
		// content, allowing harmless JSON whitespace only.
		x, e := canonical.Marshal(left)
		if e != nil {
			return false
		}
		y, e := canonical.Marshal(right)
		return e == nil && string(x) == string(y)
	}
	if !equal(root["canvas"], source["canvas"]) || !equal(root["transform_chain"], source["transform_chain"]) {
		return v2input.ErrConflict
	}
	if plan.prepared.published.anchor.operation == "scene.parse" {
		for _, key := range []string{"image", "labels", "canvas", "transform_chain", "classes", "model_id", "model_lock_id", "backend", "preprocessing_id", "confidence", "warnings"} {
			if len(root[key]) == 0 || string(root[key]) == "null" {
				return v2input.ErrInvalid
			}
		}
		var labels struct {
			Image         v2input.Ref `json:"image"`
			Labels        v2input.Ref `json:"labels"`
			Classes       int         `json:"classes"`
			ModelID       string      `json:"model_id"`
			ModelLock     string      `json:"model_lock_id"`
			Backend       string      `json:"backend"`
			Preprocessing string      `json:"preprocessing_id"`
			Confidence    float64     `json:"confidence"`
			Warnings      []string    `json:"warnings"`
		}
		if len(root) != 11 || json.Unmarshal(raw, &labels) != nil || labels.Image != plan.prepared.published.anchor.source.Source || labels.Labels != result.Pixels || result.Invalid != nil || labels.Classes != 150 || labels.ModelID != "segformer_b0_ade" || labels.Backend != expected.Backend || labels.Preprocessing != "segformer_ade512_official_v1" || math.IsNaN(labels.Confidence) || math.IsInf(labels.Confidence, 0) || labels.Confidence < 0 || labels.Confidence > 1 || string(root["warnings"]) == "null" || len(labels.Warnings) != 0 {
			return v2input.ErrInvalid
		}
		var params struct {
			Model string `json:"model_lock_id"`
		}
		if json.Unmarshal([]byte(plan.prepared.published.body), &params) != nil || labels.ModelLock != params.Model {
			return v2input.ErrConflict
		}
	} else {
		for _, key := range []string{"artifact", "float32_shape", "invalid_mask", "canvas", "transform_chain", "normalization"} {
			if len(root[key]) == 0 || (key != "invalid_mask" && string(root[key]) == "null") {
				return v2input.ErrInvalid
			}
		}
		var depth struct {
			Pixels        v2input.Ref  `json:"artifact"`
			Shape         []int64      `json:"float32_shape"`
			Invalid       *v2input.Ref `json:"invalid_mask"`
			Normalization string       `json:"normalization"`
		}
		metadata, err := v2input.ResolveSemanticSourceMetadata(plan.prepared.published.anchor.source, []byte(plan.prepared.published.anchor.sourceBody))
		if err != nil || len(root) != 6 || json.Unmarshal(raw, &depth) != nil || depth.Pixels != result.Pixels || depth.Normalization != "minmax_valid" || len(depth.Shape) != 2 || depth.Shape[0] != metadata.Height || depth.Shape[1] != metadata.Width || (depth.Invalid == nil) != (result.Invalid == nil) {
			return v2input.ErrInvalid
		}
		if depth.Invalid != nil && *depth.Invalid != *result.Invalid {
			return v2input.ErrConflict
		}
	}
	return nil
}
