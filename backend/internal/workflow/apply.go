package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

// applyResolveJobOverride seeds the request-level strength below explicit job
// and per-image overrides. Missing strength preserves historical frozen runs.
func applyResolveJobOverride(strength *float64, raw json.RawMessage) (json.RawMessage, error) {
	if strength == nil {
		return raw, nil
	}
	if math.IsNaN(*strength) || math.IsInf(*strength, 0) || *strength < 0 || *strength > 1 {
		return nil, errors.New("invalid apply strength")
	}
	body := map[string]json.RawMessage{"schema_version": json.RawMessage(`1`)}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &body); err != nil || body == nil {
			return nil, errors.New("invalid apply job override")
		}
	}
	modules := map[string]json.RawMessage{}
	if value, ok := body["modules"]; ok {
		if err := json.Unmarshal(value, &modules); err != nil || modules == nil {
			return nil, errors.New("invalid apply job modules")
		}
	}
	if value, ok := modules["strength"]; !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		value, err := canonical.Marshal(*strength)
		if err != nil {
			return nil, err
		}
		modules["strength"] = value
	}
	value, err := canonical.Marshal(modules)
	if err != nil {
		return nil, err
	}
	body["modules"] = value
	return canonical.Marshal(body)
}

type ApplyLease struct {
	FrozenPlan                                 []ApplyV4Stage
	PlanContractVersion                        int
	PlanRestartRequested                       bool
	TenantID, RunID, SourceCollectionID, Token uuid.UUID
	StyleVersionID                             *uuid.UUID
	Phase                                      string
	ItemCount                                  int
	GraphVersion                               int
	StageIndex                                 *int
}
type applyInput struct {
	Ordinal                    int
	ItemID, AssetID, VersionID uuid.UUID
	Image                      styleArtifact
	Style                      *styleArtifact
	Payload, Config            json.RawMessage
	Working                    review.WorkingImage
	Metadata                   json.RawMessage
	Sensor                     *SensorAlignment
}
type applyBoundItemOverride struct {
	ItemID         uuid.UUID       `json:"item_id"`
	AssetVersionID uuid.UUID       `json:"asset_version_id"`
	InputSHA256    string          `json:"input_sha256"`
	Body           json.RawMessage `json:"body"`
}

func boundAnalysisOverrides(items []applyInput, decisionSHA string,
	input []applyBoundItemOverride) (map[uuid.UUID]json.RawMessage, error) {
	if len(decisionSHA) != 64 || len(input) != len(items) || len(items) == 0 {
		return nil, errors.New("invalid analysis apply override count")
	}
	for _, char := range decisionSHA {
		if char < '0' || char > '9' && (char < 'a' || char > 'f') {
			return nil, errors.New("invalid analysis decision digest")
		}
	}
	byItem := make(map[uuid.UUID]applyInput, len(items))
	for _, item := range items {
		if item.ItemID == uuid.Nil || byItem[item.ItemID].ItemID != uuid.Nil {
			return nil, errors.New("duplicate analysis source item")
		}
		byItem[item.ItemID] = item
	}
	result := make(map[uuid.UUID]json.RawMessage, len(input))
	for _, entry := range input {
		item, found := byItem[entry.ItemID]
		if !found || result[entry.ItemID] != nil ||
			entry.AssetVersionID != item.VersionID ||
			entry.InputSHA256 != item.Image.SHA256 ||
			len(entry.Body) < 2 || len(entry.Body) > 1<<20 {
			return nil, errors.New("analysis override item binding mismatch")
		}
		var body struct {
			InputSHA256 string `json:"input_sha256"`
		}
		canonicalBody, err := canonical.Bytes(entry.Body)
		if err != nil || !bytes.Equal(canonicalBody, entry.Body) ||
			json.Unmarshal(entry.Body, &body) != nil || body.InputSHA256 != entry.InputSHA256 {
			return nil, errors.New("invalid analysis override source")
		}
		result[entry.ItemID] = entry.Body
	}
	return result, nil
}

type applyOutput struct {
	Ordinal        int
	ItemID, TaskID uuid.UUID
	Artifact       styleArtifact
	Route          string
}

type applyDustDecision struct {
	GroupOrdinal int
	TaskID       uuid.UUID
	Proposal     styleArtifact
	Selection    json.RawMessage
	Route        string
}

func (s ApplyStore) DefectOutputs(ctx context.Context, l ApplyLease) ([]applyOutput, error) {
	rows, err := s.DB.Query(ctx, `SELECT ordinal,item_id,task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type,route FROM photo.apply_v4_defect_outputs($1,$2,$3)`, l.TenantID, l.RunID, l.Token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []applyOutput
	for rows.Next() {
		var v applyOutput
		if err = rows.Scan(&v.Ordinal, &v.ItemID, &v.TaskID, &v.Artifact.ArtifactID, &v.Artifact.SHA256, &v.Artifact.Bytes, &v.Artifact.Bucket, &v.Artifact.Key, &v.Artifact.VersionID, &v.Artifact.MediaType, &v.Route); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type applyNodePlan struct {
	ID              uuid.UUID            `json:"id"`
	Phase           string               `json:"phase"`
	Ordinal         int                  `json:"ordinal"`
	SourceItemID    uuid.UUID            `json:"source_item_id"`
	SourceAssetID   uuid.UUID            `json:"source_asset_id"`
	SourceVersionID uuid.UUID            `json:"source_version_id"`
	NodeKey         string               `json:"node_key"`
	Operation       string               `json:"operation"`
	Inputs          []review.ArtifactRef `json:"inputs"`
	Prerequisites   []uuid.UUID          `json:"prerequisites"`
	ParamsSHA       string               `json:"params_sha256"`
	Params          json.RawMessage      `json:"params"`
}

type ApplyStore struct{ DB styleAnalysisDB }

func (s ApplyStore) Claim(ctx context.Context) (*ApplyLease, error) {
	if s.DB == nil {
		return nil, errors.New("apply database required")
	}
	var cancelled int
	if err := s.DB.QueryRow(ctx, `SELECT photo.cancel_unsupported_apply_plans()`).Scan(&cancelled); err != nil {
		return nil, err
	}
	if cancelled > 0 {
		return nil, nil
	}
	v := new(ApplyLease)
	var until any
	var source *uuid.UUID
	err := s.DB.QueryRow(ctx, `SELECT tenant_id,run_id,source_collection_id,style_version_id,phase,item_count,lease_token,lease_until,graph_version,stage_index FROM photo.claim_apply_build_versioned()`).Scan(&v.TenantID, &v.RunID, &source, &v.StyleVersionID, &v.Phase, &v.ItemCount, &v.Token, &until, &v.GraphVersion, &v.StageIndex)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if source != nil {
		v.SourceCollectionID = *source
	}
	if v.TenantID == uuid.Nil || v.RunID == uuid.Nil || v.Token == uuid.Nil || v.ItemCount < 1 || v.ItemCount > 5000 || v.GraphVersion != 3 && v.GraphVersion != 4 || v.GraphVersion == 3 && v.StageIndex != nil {
		return nil, errors.New("invalid apply lease")
	}
	if v.GraphVersion == 4 {
		if err = s.loadFrozenPlan(ctx, v); err != nil {
			return nil, err
		}
		if v.PlanRestartRequested {
			return nil, nil
		}
	}
	return v, nil
}
func (s ApplyStore) Renew(ctx context.Context, l ApplyLease) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.renew_apply_build($1,$2)`, l.RunID, l.Token).Scan(&ok)
	return ok, e
}
func (s ApplyStore) Advance(ctx context.Context, l ApplyLease, to string) (bool, error) {
	var ok bool
	query := `SELECT photo.advance_apply_build($1,$2,$3,$4)`
	args := []any{l.RunID, l.Token, l.Phase, to}
	if l.GraphVersion == 4 {
		query = `SELECT photo.advance_apply_v4_build($1,$2)`
		args = []any{l.RunID, l.Token}
	}
	e := s.DB.QueryRow(ctx, query, args...).Scan(&ok)
	return ok, e
}
func (s ApplyStore) SetGroupCount(ctx context.Context, l ApplyLease, count int) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.set_apply_v4_group_count($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, count).Scan(&ok)
	return ok, err
}
func (s ApplyStore) Fail(ctx context.Context, l ApplyLease, code string) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.fail_apply_build($1,$2,$3)`, l.RunID, l.Token, code).Scan(&ok)
	return ok, e
}
func (s ApplyStore) Adopt(ctx context.Context, l ApplyLease) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.adopt_apply_results($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&ok)
	return ok, e
}
func (s ApplyStore) Inputs(ctx context.Context, l ApplyLease) ([]applyInput, error) {
	function := "photo.apply_inputs"
	if l.GraphVersion == 4 {
		function = "photo.apply_v4_inputs_with_metadata"
	}
	columns := `ordinal,item_id,asset_id,asset_version_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type,payload,config_snapshot,style_artifact_id,style_sha256,style_bytes`
	if l.GraphVersion == 4 {
		columns += `,asset_metadata`
	}
	rows, e := s.DB.Query(ctx, `SELECT `+columns+` FROM `+function+`($1,$2,$3)`, l.TenantID, l.RunID, l.Token)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []applyInput
	for rows.Next() {
		var v applyInput
		var styleID *uuid.UUID
		var styleSHA *string
		var styleBytes *int64
		dest := []any{&v.Ordinal, &v.ItemID, &v.AssetID, &v.VersionID, &v.Image.ArtifactID, &v.Image.SHA256, &v.Image.Bytes, &v.Image.Bucket, &v.Image.Key, &v.Image.VersionID, &v.Image.MediaType, &v.Payload, &v.Config, &styleID, &styleSHA, &styleBytes}
		if l.GraphVersion == 4 {
			dest = append(dest, &v.Metadata)
		}
		e = rows.Scan(dest...)
		if e != nil {
			return nil, e
		}
		// The style object locator is loaded separately below; scan placeholders keep the SQL result exact.
		if styleID != nil && styleSHA != nil && styleBytes != nil {
			v.Style = &styleArtifact{ArtifactID: *styleID, SHA256: *styleSHA, Bytes: *styleBytes}
		}
		out = append(out, v)
	}
	if e = rows.Err(); e != nil {
		return nil, e
	}
	rows.Close() // finish the input stream before a locator query on the same pgx connection
	if len(out) != l.ItemCount {
		return nil, errors.New("apply input count mismatch")
	}
	// The accepted style artifact shares the configured bucket, but its immutable locator must be fetched exactly.
	for i := range out {
		if out[i].Style == nil {
			continue
		}
		var located styleArtifact
		located, e = s.inputStyleArtifact(ctx, l, out[i].Style.ref())
		if e == nil && located.Bytes != out[i].Style.Bytes { return nil, errors.New("apply style locator byte binding mismatch") }
		if e == nil { out[i].Style = &located }
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (s ApplyStore) Outputs(ctx context.Context, l ApplyLease, op, role string) ([]applyOutput, error) {
	function := "photo.apply_stage_outputs"
	if l.GraphVersion == 4 {
		function = "photo.apply_v4_stage_outputs"
	}
	rows, e := s.DB.Query(ctx, `SELECT ordinal,item_id,task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM `+function+`($1,$2,$3,$4,$5)`, l.TenantID, l.RunID, l.Token, op, role)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []applyOutput
	for rows.Next() {
		var v applyOutput
		if e = rows.Scan(&v.Ordinal, &v.ItemID, &v.TaskID, &v.Artifact.ArtifactID, &v.Artifact.SHA256, &v.Artifact.Bytes, &v.Artifact.Bucket, &v.Artifact.Key, &v.Artifact.VersionID, &v.Artifact.MediaType); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s ApplyStore) Register(ctx context.Context, l ApplyLease, n applyNodePlan) (bool, error) {
	// Root nodes have no predecessors, but SQL registration requires a JSON
	// array. Normalize the value copy so a nil Go slice cannot become null.
	if n.Prerequisites == nil {
		n.Prerequisites = []uuid.UUID{}
	}
	raw, e := canonical.Marshal(n)
	if e != nil {
		return false, e
	}
	var ok bool
	function := "photo.register_apply_node"
	if l.GraphVersion == 4 {
		function = "photo.register_apply_v4_node"
	} else if n.Phase == "quality" {
		function = "photo.register_apply_quality_node"
	}
	e = s.DB.QueryRow(ctx, `SELECT `+function+`($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, raw).Scan(&ok)
	return ok, e
}

func (s ApplyStore) RegisterV4Plan(ctx context.Context, l ApplyLease, plan []ApplyV4Stage) (bool, error) {
	raw, err := canonical.Marshal(plan)
	if err != nil {
		return false, err
	}
	sum := sha256.Sum256(raw)
	var ok bool
	err = s.DB.QueryRow(ctx, `SELECT photo.register_apply_v4_plan($1,$2,$3,$4,$5)`, l.TenantID, l.RunID, l.Token, hex.EncodeToString(sum[:]), raw).Scan(&ok)
	return ok, err
}
func (s ApplyStore) ProcessDefectReviews(ctx context.Context, l ApplyLease) (string, error) {
	if l.GraphVersion != 4 || l.Phase != "defects_review" {
		return "", errors.New("invalid defects review lease")
	}
	var outcome string
	err := s.DB.QueryRow(ctx, `SELECT photo.process_apply_v4_defect_reviews($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&outcome)
	return outcome, err
}
func (s ApplyStore) ProcessCandidateReviews(ctx context.Context, l ApplyLease) (string, error) {
	if l.GraphVersion != 4 || l.Phase != "background_review" && l.Phase != "inpaint_review" && l.Phase != "generation_review" && l.Phase != "composition_review" {
		return "", errors.New("invalid candidate review lease")
	}
	var outcome string
	err := s.DB.QueryRow(ctx, `SELECT photo.process_apply_v4_candidate_reviews($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&outcome)
	return outcome, err
}
func (s ApplyStore) ProcessDustReviews(ctx context.Context, l ApplyLease) (string, error) {
	if l.GraphVersion != 4 || l.Phase != "dust_review" {
		return "", errors.New("invalid dust review lease")
	}
	var outcome string
	err := s.DB.QueryRow(ctx, `SELECT photo.process_apply_v4_dust_reviews($1,$2,$3)`, l.TenantID, l.RunID, l.Token).Scan(&outcome)
	return outcome, err
}
func (s ApplyStore) DustDecisions(ctx context.Context, l ApplyLease) ([]applyDustDecision, error) {
	rows, err := s.DB.Query(ctx, `SELECT group_ordinal,task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type,selection,route FROM photo.apply_v4_dust_decisions($1,$2,$3)`, l.TenantID, l.RunID, l.Token)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []applyDustDecision
	for rows.Next() {
		var v applyDustDecision
		if err = rows.Scan(&v.GroupOrdinal, &v.TaskID, &v.Proposal.ArtifactID, &v.Proposal.SHA256, &v.Proposal.Bytes, &v.Proposal.Bucket, &v.Proposal.Key, &v.Proposal.VersionID, &v.Proposal.MediaType, &v.Selection, &v.Route); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s ApplyStore) CandidateOutputs(ctx context.Context, l ApplyLease, phase string) ([]applyOutput, error) {
	rows, err := s.DB.Query(ctx, `SELECT ordinal,item_id,task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type,route FROM photo.apply_v4_candidate_outputs($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, phase)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []applyOutput
	for rows.Next() {
		var v applyOutput
		if err = rows.Scan(&v.Ordinal, &v.ItemID, &v.TaskID, &v.Artifact.ArtifactID, &v.Artifact.SHA256, &v.Artifact.Bytes, &v.Artifact.Bucket, &v.Artifact.Key, &v.Artifact.VersionID, &v.Artifact.MediaType, &v.Route); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s ApplyStore) Materialize(ctx context.Context, l ApplyLease, t PreparedTask) (bool, error) {
	raw, e := canonical.Marshal(t)
	if e != nil {
		return false, e
	}
	var ok bool
	e = s.DB.QueryRow(ctx, `SELECT photo.materialize_apply_task($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, raw).Scan(&ok)
	return ok, e
}

type ApplyDocuments struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (r ApplyDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r.DB == nil || s.Purpose != "task_params" {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	e := r.DB.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_apply_document($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Run, s.Owner, sha, size, r.Bucket).Scan(&outcome, &id, &token, &ready)
	if e != nil {
		return artifactjson.Reservation{}, e
	}
	if outcome == "conflict" {
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	}
	if outcome == "quota" {
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	}
	if outcome == "stale" || outcome == "forbidden" {
		return artifactjson.Reservation{}, artifactjson.ErrStale
	}
	if id == nil || token == nil {
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
	}
	v := artifactjson.Reservation{ArtifactID: *id, Token: *token}
	if ready {
		v.Ready = &artifactjson.Ref{ID: *id, SHA256: sha}
	}
	return v, nil
}
func (r ApplyDocuments) Complete(ctx context.Context, s artifactjson.Scope, t artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	key, e := artifactjson.DocumentKey(s, t.ArtifactID)
	if e != nil || o.Key != key {
		return artifactjson.Ref{}, objectstore.ErrIntegrity
	}
	var ok bool
	e = r.DB.QueryRow(ctx, `SELECT photo.complete_apply_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Run, s.Owner, t.ArtifactID, t.Token, o.SHA256, o.Bytes, r.Bucket, o.VersionID).Scan(&ok)
	if e != nil {
		return artifactjson.Ref{}, e
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: t.ArtifactID, SHA256: o.SHA256}, nil
}

type ApplyCoordinator struct {
	Store     ApplyPhaseStore
	Publisher ContractPublisher
	Objects   ArtifactOpener
	Bucket    string
}

func applyProfile(pixels, inputBytes int64) (json.RawMessage, error) {
	out := pixels*12 + 2*1048576
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 512, ResourceEstimate{RAMBytes: 3 << 30, ScratchBytes: inputBytes + out, OutputBytes: out, PixelCount: pixels}})
}

// Per-operation peaks mirror worker ExitStack/reservation lifetimes. Input NPY
// envelopes are bounded by arrays.py (4096-byte header +12-byte preamble);
// 8192 also matches the worker's explicit output reservation allowance.
// Typed JSON is streamed into RAM, then staged only after array scopes close.
type applyLegacySizing struct {
	Adaptive, Alpha, Baseline bool
	LUTSize                   int // Actual resolved color LUT edge (33/65), zero for fixed color.
	Masks                     int // Materialized float32 person/region/protection masks, including repeats.
}

func applyLegacyResourceProfile(operation string, raw json.RawMessage, sizing applyLegacySizing) (json.RawMessage, error) {
	switch operation {
	case "style.resolve", "image.color", "image.local", "quality.check":
	default:
		return raw, nil
	}
	var profile struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(raw, &profile) != nil || profile.Backend != "cpu" || profile.Estimate.PixelCount < 1 || profile.Estimate.PixelCount > 200000000 || sizing.Masks < 0 || sizing.Masks > 160 || sizing.LUTSize != 0 && sizing.LUTSize != 33 && sizing.LUTSize != 65 {
		return nil, errors.New("invalid legacy Apply resource inputs")
	}
	const document int64 = 1 << 20
	const envelope int64 = 8192
	pixels := profile.Estimate.PixelCount
	rgb := 12*pixels + envelope
	mask := 4*pixels + envelope
	// Resolve's adaptive alpha reader is uint16; quality supports float32 alpha.
	alpha := 2*pixels + envelope
	lut := int64(65*65*65*3*4) + envelope
	peak, output := document, document
	switch operation {
	case "style.resolve":
		if sizing.Adaptive {
			peak = rgb
			if sizing.Alpha {
				peak += alpha
			}
			if lut > peak {
				peak = lut
			}
			if peak < document {
				peak = document
			}
			output = lut + document
		}
	case "image.color":
		peak = 2 * rgb
		if sizing.LUTSize != 0 {
			edge := int64(sizing.LUTSize)
			peak += edge*edge*edge*12 + envelope
		}
		output = rgb + 2*document
	case "image.local":
		peak = 3*rgb + int64(sizing.Masks)*mask
		output = rgb + 2*document
	case "quality.check":
		peak = rgb
		if sizing.Alpha {
			peak += mask
		}
		if sizing.Baseline {
			peak += rgb
		}
		peak += int64(sizing.Masks) * mask
	}
	if peak < document {
		peak = document
	}
	profile.Estimate.ScratchBytes = peak
	profile.Estimate.OutputBytes = output
	return canonical.Marshal(profile)
}

func applySeriesBalanceProfile(inputBytes int64,
	model, runtime, device string) (json.RawMessage, error) {
	base, err := applyProfile(1, inputBytes)
	if err != nil || model == "" && runtime == "" && device == "" {
		return base, err
	}
	for _, pin := range []string{model, runtime, device} {
		if _, err := domain.ParseDigest(pin); err != nil {
			return nil, errors.New("invalid series qualification pins")
		}
	}
	var ordinary struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(base, &ordinary) != nil {
		return nil, errors.New("invalid series resource profile")
	}
	return canonical.Marshal(struct {
		Backend          string           `json:"backend"`
		Precision        string           `json:"precision"`
		Tile             int              `json:"tile_size"`
		Estimate         ResourceEstimate `json:"resource_estimate"`
		ModelLockID      string           `json:"model_lock_id"`
		ModelLockSHA     string           `json:"model_lock_sha"`
		RuntimeLockSHA   string           `json:"runtime_lock_sha"`
		DeviceProfileSHA string           `json:"device_profile_sha"`
	}{ordinary.Backend, ordinary.Precision, ordinary.Tile,
		ordinary.Estimate, runtime, model, runtime, device})
}

// applyDustDetectionProfile admits one sequential native detector frame at a time.
func applyDustDetectionProfile(pixels, maxFramePixels, documentBytes int64, frames int) (json.RawMessage, error) {
	if frames < 5 || frames > 64 || maxFramePixels < 64 || maxFramePixels > 200000000 ||
		pixels != maxFramePixels*int64(frames) || documentBytes < 0 || documentBytes > int64(frames)*1048576 {
		return nil, errors.New("invalid dust detection resource dimensions")
	}
	ram := maxFramePixels*192 + 256*1048576
	if ram < 3<<30 {
		ram = 3 << 30
	}
	out := int64(2 * 1048576)
	scratch := pixels*12 + int64(frames)*8192 + documentBytes + out + 65536
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 512, ResourceEstimate{RAMBytes: ram, ScratchBytes: scratch, OutputBytes: out, PixelCount: pixels}})
}

// applyDustRepairProfile matches the full-frame CPU worker admission and owns
// downloaded source plus staged RGB, image/report contracts and NPY overhead.
func applyDustRepairProfile(pixels, documentBytes int64) (json.RawMessage, error) {
	if pixels < 1 || pixels > 200000000 || documentBytes < 0 || documentBytes > 1048576 {
		return nil, errors.New("invalid dust repair resource dimensions")
	}
	ram := pixels*128 + 256*1048576
	if ram < 3<<30 {
		ram = 3 << 30
	}
	out := pixels*12 + 2*1048576
	scratch := pixels*12 + 8192 + documentBytes + out + 65536
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 512, ResourceEstimate{RAMBytes: ram, ScratchBytes: scratch, OutputBytes: out, PixelCount: pixels}})
}

// applyCropProfile accounts for full source conversion and transformed output,
// including downloaded input arrays and uint16 alpha. Native math is unchanged.
func applyCropProfile(sourcePixels, outputPixels, documentBytes int64, hasAlpha bool) (json.RawMessage, error) {
	if sourcePixels < 1 || sourcePixels > 200000000 || outputPixels < 1 || outputPixels > 200000000 || documentBytes < 0 || documentBytes > 1048576 {
		return nil, errors.New("invalid crop resource dimensions")
	}
	sourceRAM, outputRAM, bpp := int64(64), int64(96), int64(12)
	if hasAlpha {
		sourceRAM += 8
		outputRAM += 24
		bpp += 2
	}
	ram := sourceRAM*sourcePixels + outputRAM*outputPixels + 128*1048576
	if ram < 3<<30 {
		ram = 3 << 30
	}
	output := bpp*outputPixels + 2*1048576
	scratch := bpp*sourcePixels + 8192 + documentBytes + output + 65536
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 512, ResourceEstimate{RAMBytes: ram, ScratchBytes: scratch, OutputBytes: output, PixelCount: outputPixels}})
}

func applyModelProfile(backend, lock string, pixels, inputBytes int64) (json.RawMessage, error) {
	out := pixels*4 + 2*1048576
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		ModelLock string           `json:"model_lock_id"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{backend, "float32", 512, lock, ResourceEstimate{RAMBytes: 2 << 30, VRAMBytes: map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[backend], ScratchBytes: inputBytes + pixels*8 + 4*1048576, OutputBytes: out, PixelCount: pixels}})
}
func applyUpscaleProfile(backend, lock string, inputPixels, inputBytes int64, scale int, hasAlpha bool) (json.RawMessage, error) {
	if inputPixels < 1 || scale != 2 && scale != 4 || inputPixels > 200000000/int64(scale*scale) {
		return nil, errors.New("upscale pixel limit exceeded")
	}
	outputPixels := inputPixels * int64(scale*scale)
	bytesPerPixel := int64(12)
	if hasAlpha {
		bytesPerPixel += 2
	}
	outputBytes := outputPixels*bytesPerPixel + 2*1048576
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		ModelLock string           `json:"model_lock_id"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{backend, "float32", 256, lock, ResourceEstimate{RAMBytes: 2 << 30, VRAMBytes: map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[backend], ScratchBytes: inputBytes + outputBytes + 4*1048576, OutputBytes: outputBytes, PixelCount: outputPixels}})
}
func applyBackgroundProfile(pixels, inputBytes int64, transparent bool) (json.RawMessage, error) {
	bytesPerPixel := int64(12)
	if transparent {
		bytesPerPixel += 2
	}
	out := pixels*bytesPerPixel + 2*1048576
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{"cpu", "float32", 512, ResourceEstimate{RAMBytes: 3 << 30, ScratchBytes: inputBytes + out, OutputBytes: out, PixelCount: pixels}})
}
func refsForImage(manifest review.ArtifactRef, w review.WorkingImage) []review.ArtifactRef {
	r := []review.ArtifactRef{manifest, w.Pixels}
	if w.Alpha != nil {
		r = append(r, *w.Alpha)
	}
	return r
}

func validateApplyPersonMask(mask review.MaskRef, image review.WorkingImage, lock string) error {
	if mask.Selector != "person" || mask.Canvas != image.Canvas || !bytes.Equal(mask.TransformChain, image.TransformChain) || mask.ModelLockID == nil || *mask.ModelLockID != lock {
		return errors.New("invalid person mask binding")
	}
	return nil
}

type applyRelativeDepth struct {
	Artifact       review.ArtifactRef  `json:"artifact"`
	InvalidMask    *review.ArtifactRef `json:"invalid_mask"`
	Canvas         review.Canvas       `json:"canvas"`
	TransformChain json.RawMessage     `json:"transform_chain"`
	Normalization  string              `json:"normalization"`
}

type applyOverrideMaskBinding struct {
	BindingID string         `json:"binding_id"`
	Purpose   string         `json:"purpose"`
	Mask      review.MaskRef `json:"mask"`
	Confirmed bool           `json:"confirmed"`
	Strength  float64        `json:"strength"`
}

func readApplyJSON(ctx context.Context, objects ArtifactOpener, bucket string, a styleArtifact) ([]byte, error) {
	return readApplyJSONBound(ctx, objects, bucket, a, 1048576)
}
func readApplyJSONBound(ctx context.Context, objects ArtifactOpener, bucket string, a styleArtifact, maximum int64) ([]byte, error) {
	if objects == nil || a.Bucket != bucket || !a.valid() || a.Bytes > maximum {
		return nil, objectstore.ErrInvalid
	}
	r, e := objects.Open(ctx, a.object())
	if e != nil {
		return nil, e
	}
	defer r.Close()
	raw, e := io.ReadAll(io.LimitReader(r, maximum+1))
	if e != nil {
		return nil, e
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != a.Bytes || hex.EncodeToString(sum[:]) != a.SHA256 {
		return nil, objectstore.ErrIntegrity
	}
	return canonical.Bytes(raw)
}

func (c ApplyCoordinator) materializeNode(ctx context.Context, l ApplyLease, reg Registry, plan applyNodePlan, profile json.RawMessage) error {
	ok, err := c.Store.Register(ctx, l, plan)
	if err != nil || !ok {
		return errors.New("apply node registration rejected")
	}
	ref, err := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: plan.ID, Purpose: "task_params"}, plan.Params)
	if err != nil {
		return err
	}
	task, err := prepareApplyMaterialization(reg, plan, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}, profile)
	if err != nil {
		return err
	}
	ok, err = c.Store.Materialize(ctx, l, task)
	if err != nil || !ok {
		return errors.New("apply task materialization rejected")
	}
	return nil
}

func parseApplyQualityStatus(raw []byte) (string, error) {
	var value struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Status != "pass" && value.Status != "review" && value.Status != "fail" {
		return "", errors.New("invalid apply quality report")
	}
	return value.Status, nil
}

func (c ApplyCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	l, e := c.Store.Claim(ctx)
	if e != nil || l == nil {
		return false, e
	}
	if l.Phase == "adopt" {
		reports, err := c.Store.Outputs(ctx, *l, "quality.check", "quality_report")
		if err != nil || len(reports) != l.ItemCount {
			if err == nil {
				err = errors.New("apply quality report count mismatch")
			}
			return false, err
		}
		for _, report := range reports {
			raw, readErr := readApplyJSON(ctx, c.Objects, c.Bucket, report.Artifact)
			if readErr != nil {
				return false, readErr
			}
			status, parseErr := parseApplyQualityStatus(raw)
			if parseErr != nil {
				return false, parseErr
			}
			if status == "fail" {
				ok, failErr := c.Store.Fail(ctx, *l, "QUALITY_FAILED")
				if failErr != nil || !ok {
					if failErr == nil {
						failErr = errors.New("apply quality failure commit rejected")
					}
					return false, failErr
				}
				return true, nil
			}
		}
		ok, err := c.Store.Adopt(ctx, *l)
		if err != nil || !ok {
			if err == nil {
				err = errors.New("apply result adoption rejected")
			}
			return false, err
		}
		return true, nil
	}
	return c.build(ctx, *l)
}
func (c ApplyCoordinator) build(ctx context.Context, l ApplyLease) (bool, error) {
	items, e := c.Store.Inputs(ctx, l)
	if e != nil {
		return false, e
	}
	reg, e := LoadRegistry()
	if e != nil {
		return false, e
	}
	for i := range items {
		items[i].Working, e = readWorking(ctx, c.Objects, c.Bucket, items[i].Image)
		if e != nil {
			return false, e
		}
		if l.GraphVersion == 4 {
			items[i].Sensor, e = parseApplySensorAlignment(items[i].Metadata, items[i].Working)
			if e != nil {
				return false, e
			}
		}
	}
	var cfg struct {
		StyleConfig struct {
			Mode       string `json:"mode"`
			Extensions struct {
				Restoration struct {
					Denoise         string  `json:"denoise"`
					DenoiseStrength float64 `json:"denoise_strength"`
					Upscale         string  `json:"upscale"`
					SensorDust      string  `json:"sensor_dust"`
				} `json:"restoration"`
				Background struct {
					Mode           string    `json:"mode"`
					AssetID        string    `json:"asset_id"`
					SolidRGB       []float64 `json:"solid_rgb"`
					BlurStrength   float64   `json:"blur_strength"`
					ProtectSubject bool      `json:"protect_subject"`
				} `json:"background"`
				Inpainting struct {
					Enabled bool `json:"enabled"`
				} `json:"inpainting"`
				Generation struct {
					Enabled  bool    `json:"enabled"`
					Prompt   string  `json:"prompt"`
					Negative string  `json:"negative_prompt"`
					Seed     uint32  `json:"seed"`
					Steps    int     `json:"steps"`
					Guidance float64 `json:"guidance_scale"`
					Strength float64 `json:"strength"`
				} `json:"generation"`
				Composition struct {
					Enabled          bool    `json:"enabled"`
					Aspect           []int   `json:"aspect"`
					Headroom         float64 `json:"headroom"`
					Rotate           float64 `json:"rotate_limit_deg"`
					OutpaintPolicy   string  `json:"outpaint_policy"`
					OutpaintPrompt   string  `json:"outpaint_prompt"`
					OutpaintNegative string  `json:"outpaint_negative_prompt"`
					OutpaintSeed     uint32  `json:"outpaint_seed"`
					OutpaintGuidance float64 `json:"outpaint_guidance_scale"`
				} `json:"composition"`
			} `json:"extensions"`
		} `json:"style_config"`
		PersonRequired      bool                         `json:"person_mask_required"`
		PersonBackend       string                       `json:"person_backend"`
		PersonLock          string                       `json:"person_runtime_lock_id"`
		OverrideConfig      json.RawMessage              `json:"override_config"`
		JobOverrides        json.RawMessage              `json:"job_overrides"`
		Strength            *float64                     `json:"strength"`
		GraphStyleConfig    json.RawMessage              `json:"graph_style_config"`
		AnalysisDecisionSHA string                       `json:"analysis_decision_sha"`
		ItemOverrides       []applyBoundItemOverride     `json:"item_overrides"`
		ItemMaskInputs      []applyItemMaskInput         `json:"item_mask_inputs"`
		Identity            bool                         `json:"identity_style"`
		SeriesEnabled       bool                         `json:"series_enabled"`
		SeriesModelLock     string                       `json:"series_model_lock_sha"`
		SeriesRuntimeLock   string                       `json:"series_runtime_lock_sha"`
		SeriesDeviceProfile string                       `json:"series_device_profile_sha"`
		GeometryEnabled     bool                         `json:"geometry_enabled"`
		BeautyEnabled       bool                         `json:"beauty_enabled"`
		PortraitDetail      bool                         `json:"portrait_detail_enabled"`
		DefectsEnabled      bool                         `json:"defects_enabled"`
		RestorationBackend  string                       `json:"restoration_backend"`
		RestorationLock     string                       `json:"restoration_runtime_lock_id"`
		CreativeBackend     string                       `json:"creative_backend"`
		CreativeLock        string                       `json:"creative_runtime_lock_id"`
		BackgroundAsset     *review.ApplyBackgroundAsset `json:"background_asset"`
	}
	var planConfig struct {
		StyleConfig json.RawMessage `json:"style_config"`
	}
	if len(items) == 0 || json.Unmarshal(items[0].Config, &cfg) != nil || json.Unmarshal(items[0].Config, &planConfig) != nil || cfg.StyleConfig.Mode != "fixed" && cfg.StyleConfig.Mode != "adaptive" || cfg.Identity != (items[0].Style == nil) {
		return false, errors.New("invalid apply execution config")
	}
	var itemOverride map[uuid.UUID]json.RawMessage
	if cfg.AnalysisDecisionSHA != "" {
		if l.GraphVersion != 4 || len(cfg.OverrideConfig) != 0 ||
			len(cfg.JobOverrides) != 0 {
			return false, errors.New("invalid analysis apply override config")
		}
		itemOverride, e = boundAnalysisOverrides(items, cfg.AnalysisDecisionSHA, cfg.ItemOverrides)
		if e != nil {
			return false, e
		}
	} else if len(cfg.ItemOverrides) != 0 || len(cfg.GraphStyleConfig) != 0 {
		return false, errors.New("orphan analysis apply configuration")
	}
	effectiveStyle := planConfig.StyleConfig
	if l.GraphVersion == 4 {
		effectiveStyle, e = EffectiveApplyV4Style(planConfig.StyleConfig, cfg.JobOverrides)
		if e == nil {
			effectiveStyle, e = EffectiveApplyV4Style(effectiveStyle, cfg.OverrideConfig)
		}
		if e == nil && len(cfg.GraphStyleConfig) != 0 {
			effectiveStyle, e = canonical.Bytes(cfg.GraphStyleConfig)
		}
		if e != nil || json.Unmarshal(effectiveStyle, &cfg.StyleConfig) != nil {
			return false, errors.New("invalid effective apply execution config")
		}
	}
	adaptive := cfg.StyleConfig.Mode == "adaptive"
	denoiseEnabled := cfg.StyleConfig.Extensions.Restoration.Denoise == "scunet_color_real_psnr"
	upscaleEnabled := cfg.StyleConfig.Extensions.Restoration.Upscale == "2x" || cfg.StyleConfig.Extensions.Restoration.Upscale == "4x"
	backgroundMode := cfg.StyleConfig.Extensions.Background.Mode
	backgroundEnabled := backgroundMode == "asset" || backgroundMode == "transparent" || backgroundMode == "solid" || backgroundMode == "blur" || backgroundMode == "generate"
	backgroundOp := "background.compose"
	if backgroundMode == "blur" {
		backgroundOp = "background.blur"
	} else if backgroundMode == "generate" {
		backgroundOp = "image.generate"
	}
	var overrideMasks struct {
		MaskRefs []applyOverrideMaskBinding `json:"mask_refs"`
	}
	if len(cfg.OverrideConfig) > 0 && json.Unmarshal(cfg.OverrideConfig, &overrideMasks) != nil {
		return false, errors.New("invalid apply override mask bindings")
	}
	if backgroundMode == "asset" {
		if items[0].Style == nil {
			return false, errors.New("asset background requires accepted style")
		}
		if e = validateApplyBackgroundAsset(cfg.BackgroundAsset, cfg.StyleConfig.Extensions.Background.AssetID, items[0].Style.ref()); e != nil {
			return false, e
		}
	} else if cfg.BackgroundAsset != nil {
		return false, errors.New("unexpected background asset binding")
	}
	itemMaskBindings, maskBindErr := boundApplyItemMasks(items, cfg.ItemMaskInputs)
	if maskBindErr != nil {
		return false, maskBindErr
	}
	globalMaskBindings := append([]applyOverrideMaskBinding(nil), overrideMasks.MaskRefs...)
	var activeMaskItem uuid.UUID
	creativeMasks := func(working review.WorkingImage, purpose string) (review.MaskRef, []review.MaskRef, error) {
		var target review.MaskRef
		protected := make([]review.MaskRef, 0)
		bindings := overrideMasks.MaskRefs
		if purpose == "" && len(bindings) == 0 {
			return target, protected, nil
		}
		if cfg.GeometryEnabled {
			geometry, bindErr := c.Store.Outputs(ctx, l, "geometry.body", "working_image")
			if bindErr != nil {
				return target, nil, bindErr
			}
			matching := []applyOutput{}
			for _, row := range geometry {
				if row.ItemID == activeMaskItem {
					matching = append(matching, row)
				}
			}
			geometry = matching
			if len(geometry) != 1 {
				return target, nil, errors.New("creative geometry binding count mismatch")
			}
			transforms, bindErr := c.Store.Outputs(ctx, l, "mask.transform", "mask_transform_result")
			if bindErr != nil {
				return target, nil, bindErr
			}
			selected := []applyOutput{}
			for _, row := range transforms {
				if row.ItemID == activeMaskItem {
					selected = append(selected, row)
				}
			}
			transforms = selected
			if len(transforms) != 1 || transforms[0].ItemID != geometry[0].ItemID {
				return target, nil, errors.New("creative transform binding count mismatch")
			}
			root, bindErr := readApplyJSON(ctx, c.Objects, c.Bucket, transforms[0].Artifact)
			if bindErr != nil {
				return target, nil, bindErr
			}
			bindings, bindErr = transformedApplyRegionBindings(root, bindings, geometry[0].Artifact.ref(), working)
			if bindErr != nil {
				return target, nil, bindErr
			}
		}
		for _, binding := range bindings {
			if binding.Purpose != purpose && binding.Purpose != "protected" {
				continue
			}
			if binding.Mask.Canvas != working.Canvas || !bytes.Equal(binding.Mask.TransformChain, working.TransformChain) {
				return target, nil, errors.New("creative mask geometry mismatch")
			}
			switch binding.Purpose {
			case purpose:
				if target.Artifact.ID != uuid.Nil || !binding.Confirmed || binding.Mask.Protected {
					return target, nil, errors.New("invalid creative target mask")
				}
				target = binding.Mask
			case "protected":
				if !binding.Mask.Protected {
					return target, nil, errors.New("invalid protected mask")
				}
				protected = append(protected, binding.Mask)
			}
		}
		if purpose != "" && target.Artifact.ID == uuid.Nil {
			return target, nil, errors.New("creative target mask missing")
		}
		return target, protected, nil
	}
	var v4Plan []ApplyV4Stage
	if l.GraphVersion == 4 {
		plan := l.FrozenPlan
		var planErr error
		if len(plan) == 0 {
			plan, planErr = CompileApplyV4Plan(effectiveStyle, l.ItemCount)
			if planErr == nil {
				plan, planErr = editorPrecomposeStages(items[0].Config, plan)
			}
		}
		if planErr != nil {
			return false, planErr
		}
		if len(plan) == 0 || plan[0].Phase != l.Phase && l.StageIndex == nil {
			return false, errors.New("apply v4 initial phase mismatch")
		}
		ok, registerErr := c.Store.RegisterV4Plan(ctx, l, plan)
		if registerErr != nil || !ok {
			if registerErr == nil {
				registerErr = errors.New("apply v4 plan registration rejected")
			}
			return false, registerErr
		}
		v4Plan = plan
		editorMode, editorErr := editorPrecomposeFromConfig(items[0].Config)
		if editorErr != nil {
			return false, editorErr
		}
		if editorMode != nil {
			if l.Phase == "editor_precompose" {
				return c.buildEditorPrecompose(ctx, l, reg, items, plan)
			}
			items, e = c.editorEffectInputs(ctx, l, items)
			if e != nil {
				return false, e
			}
		}

		if l.Phase == "series_pack" {
			return c.buildSeriesPacks(ctx, l, reg, items)
		}
		if len(l.Phase) >= 5 && l.Phase[:5] == "cull_" {
			return c.buildCulling(ctx, l, reg, items, effectiveStyle, cfg.PersonBackend, cfg.PersonLock)
		}
		if l.Phase == "defects_review" {
			outcome, reviewErr := c.Store.ProcessDefectReviews(ctx, l)
			if reviewErr != nil || outcome != "opened" && outcome != "activated" && outcome != "advanced" {
				if reviewErr == nil {
					reviewErr = errors.New("apply v4 defects review processing rejected")
				}
				return false, reviewErr
			}
			return true, nil
		}
		if l.Phase == "dust_review" {
			outcome, reviewErr := c.Store.ProcessDustReviews(ctx, l)
			if reviewErr != nil || outcome != "opened" && outcome != "advanced" {
				if reviewErr == nil {
					reviewErr = errors.New("apply v4 dust review processing rejected")
				}
				return false, reviewErr
			}
			return true, nil
		}
		if l.Phase == "background_review" || l.Phase == "inpaint_review" || l.Phase == "generation_review" || l.Phase == "composition_review" {
			outcome, reviewErr := c.Store.ProcessCandidateReviews(ctx, l)
			if reviewErr != nil || outcome != "opened" && outcome != "advanced" {
				if reviewErr == nil {
					reviewErr = errors.New("apply v4 candidate review processing rejected")
				}
				return false, reviewErr
			}
			return true, nil
		}
	}
	autoDust := cfg.StyleConfig.Extensions.Restoration.SensorDust == "conservative_auto"
	dustEnabled := cfg.StyleConfig.Extensions.Restoration.SensorDust == "review" || autoDust
	var effectiveAutoPolicy struct {
		Defects struct {
			Policy string `json:"policy"`
		} `json:"defects"`
	}
	if json.Unmarshal(effectiveStyle, &effectiveAutoPolicy) != nil {
		return false, errors.New("invalid conservative style")
	}
	autoDefects := cfg.DefectsEnabled && effectiveAutoPolicy.Defects.Policy == "conservative_auto"
	defectOutputs := func() ([]applyOutput, error) {
		if autoDefects {
			return c.Store.Outputs(ctx, l, "defects.auto_repair", "working_image")
		}
		return c.Store.DefectOutputs(ctx, l)
	}
	if l.GraphVersion == 4 && l.Phase == "dust_detect" {
		if !dustEnabled {
			return false, errors.New("dust detect phase not authorized")
		}
		colored, err := c.Store.Outputs(ctx, l, "image.color", "working_image")
		if err != nil || len(colored) != len(items) {
			return false, errors.New("dust color inputs missing")
		}
		byItem := map[uuid.UUID]applyOutput{}
		for _, out := range colored {
			byItem[out.ItemID] = out
		}
		groups, err := compileApplyDustGroups(items)
		if err != nil {
			return false, err
		}
		ok, err := c.Store.SetGroupCount(ctx, l, len(groups))
		if err != nil || !ok {
			return false, errors.New("apply dust group count rejected")
		}
		for ordinal, group := range groups {
			if ok, er := c.Store.Renew(ctx, l); er != nil || !ok {
				return false, errors.New("apply lease lost")
			}
			frames := make([]review.ArtifactRef, len(group.Items))
			inputs := make([]review.ArtifactRef, 0, len(group.Items)*2)
			prereq := make([]uuid.UUID, len(group.Items))
			var pixels, maxFramePixels, inputBytes int64
			for i, item := range group.Items {
				out, found := byItem[item.ItemID]
				if !found {
					return false, errors.New("dust group color binding missing")
				}
				working, er := readWorking(ctx, c.Objects, c.Bucket, out.Artifact)
				if er != nil {
					return false, er
				}
				frames[i] = out.Artifact.ref()
				inputs = append(inputs, refsForImage(frames[i], working)...)
				prereq[i] = out.TaskID
				framePixels := int64(working.Canvas.Width) * int64(working.Canvas.Height)
				pixels += framePixels
				if framePixels > maxFramePixels {
					maxFramePixels = framePixels
				}
				inputBytes += out.Artifact.Bytes
			}
			s := group.Items[0].Sensor
			params, _ := canonical.Marshal(struct {
				Frames       []review.ArtifactRef `json:"frames"`
				Camera       string               `json:"camera_id"`
				SensorWidth  int64                `json:"sensor_width"`
				SensorHeight int64                `json:"sensor_height"`
				RawCrop      struct {
					X      int64 `json:"x"`
					Y      int64 `json:"y"`
					Width  int64 `json:"width"`
					Height int64 `json:"height"`
				} `json:"raw_crop"`
				Orientation int     `json:"orientation"`
				Recurrence  float64 `json:"recurrence"`
			}{Frames: frames, Camera: s.CameraID, SensorWidth: s.SensorWidth, SensorHeight: s.SensorHeight, RawCrop: struct {
				X      int64 `json:"x"`
				Y      int64 `json:"y"`
				Width  int64 `json:"width"`
				Height int64 `json:"height"`
			}{s.CropX, s.CropY, s.CropWidth, s.CropHeight}, Orientation: 1, Recurrence: .8})
			key := fmt.Sprintf("sensor.dust/%04d", ordinal)
			id := nodeID(l.RunID, key)
			sum := sha256.Sum256(params)
			first := group.Items[0]
			profile, er := applyDustDetectionProfile(pixels, maxFramePixels, inputBytes, len(frames))
			if er != nil {
				return false, er
			}
			plan := applyNodePlan{id, l.Phase, ordinal, first.ItemID, first.AssetID, first.VersionID, key, "sensor.dust", inputs, prereq, hex.EncodeToString(sum[:]), params}
			if er = c.materializeNode(ctx, l, reg, plan, profile); er != nil {
				return false, er
			}
		}
		ok, err = c.Store.Advance(ctx, l, "")
		if err != nil || !ok {
			return false, errors.New("apply dust detection advance rejected")
		}
		return true, nil
	}
	if l.GraphVersion == 4 && l.Phase == "dust_repair" && autoDust {
		return c.materializeConservativeDust(ctx, l, reg, items, cfg.GeometryEnabled, cfg.PersonLock, creativeMasks)
	}
	if l.GraphVersion == 4 && l.Phase == "dust_repair" {
		if !dustEnabled {
			return false, errors.New("dust repair phase not authorized")
		}
		colored, err := c.Store.Outputs(ctx, l, "image.color", "working_image")
		if err != nil || len(colored) != len(items) {
			return false, errors.New("dust repair color inputs missing")
		}
		byItem := map[uuid.UUID]applyOutput{}
		for _, out := range colored {
			byItem[out.ItemID] = out
		}
		groups, err := compileApplyDustGroups(items)
		if err != nil {
			return false, err
		}
		decisions, err := c.Store.DustDecisions(ctx, l)
		if err != nil || len(decisions) != len(groups) {
			return false, errors.New("dust decision count mismatch")
		}
		byGroup := map[int]applyDustDecision{}
		for _, d := range decisions {
			byGroup[d.GroupOrdinal] = d
		}
		itemDecision := map[uuid.UUID]applyDustDecision{}
		for ordinal, g := range groups {
			d, found := byGroup[ordinal]
			if !found {
				return false, errors.New("dust group decision missing")
			}
			for _, item := range g.Items {
				itemDecision[item.ItemID] = d
			}
		}
		for _, item := range items {
			out := byItem[item.ItemID]
			working, er := readWorking(ctx, c.Objects, c.Bucket, out.Artifact)
			if er != nil {
				return false, er
			}
			inputs := refsForImage(out.Artifact.ref(), working)
			prereq := []uuid.UUID{out.TaskID}
			var proposal *review.ArtifactRef
			var selection json.RawMessage
			if d, found := itemDecision[item.ItemID]; found {
				v := d.Proposal.ref()
				proposal = &v
				selection = d.Selection
				inputs = append(inputs, v)
				prereq = append(prereq, d.TaskID)
			}
			params, _ := canonical.Marshal(struct {
				Image    review.ArtifactRef  `json:"image"`
				Proposal *review.ArtifactRef `json:"proposal,omitempty"`
				Review   json.RawMessage     `json:"review,omitempty"`
			}{out.Artifact.ref(), proposal, selection})
			key := fmt.Sprintf("sensor.dust_repair/%04d", item.Ordinal)
			id := nodeID(l.RunID, key)
			sum := sha256.Sum256(params)
			plan := applyNodePlan{id, l.Phase, item.Ordinal, item.ItemID, item.AssetID, item.VersionID, key, "sensor.dust_repair", inputs, prereq, hex.EncodeToString(sum[:]), params}
			profile, er := applyDustRepairProfile(int64(working.Canvas.Width*working.Canvas.Height), out.Artifact.Bytes)
			if er != nil {
				return false, er
			}
			if er = c.materializeNode(ctx, l, reg, plan, profile); er != nil {
				return false, er
			}
		}
		ok, err := c.Store.Advance(ctx, l, "")
		if err != nil || !ok {
			return false, errors.New("apply dust repair advance rejected")
		}
		return true, nil
	}
	if cfg.PersonRequired {
		if cfg.PersonBackend != "cpu" && cfg.PersonBackend != "cuda" && cfg.PersonBackend != "mps" {
			return false, errors.New("invalid person backend")
		}
		if _, e = domain.ParseDigest(cfg.PersonLock); e != nil {
			return false, e
		}
	} else if cfg.PersonBackend != "" || cfg.PersonLock != "" {
		return false, errors.New("unexpected person runtime binding")
	}
	if denoiseEnabled || upscaleEnabled {
		if cfg.RestorationBackend != "cpu" && cfg.RestorationBackend != "cuda" && cfg.RestorationBackend != "mps" {
			return false, errors.New("invalid restoration backend")
		}
		if _, e = domain.ParseDigest(cfg.RestorationLock); e != nil {
			return false, e
		}
	} else if cfg.RestorationBackend != "" || cfg.RestorationLock != "" {
		return false, errors.New("unexpected restoration runtime binding")
	}
	creativeEnabled := cfg.StyleConfig.Extensions.Inpainting.Enabled || cfg.StyleConfig.Extensions.Generation.Enabled || backgroundMode == "generate" || cfg.StyleConfig.Extensions.Composition.Enabled && cfg.StyleConfig.Extensions.Composition.OutpaintPolicy == "review"
	compositionEnabled := cfg.StyleConfig.Extensions.Composition.Enabled
	if creativeEnabled {
		if cfg.CreativeBackend != "cpu" && cfg.CreativeBackend != "cuda" && cfg.CreativeBackend != "mps" {
			return false, errors.New("invalid creative backend")
		}
		if _, e = domain.ParseDigest(cfg.CreativeLock); e != nil {
			return false, e
		}
	} else if cfg.CreativeBackend != "" || cfg.CreativeLock != "" {
		return false, errors.New("unexpected creative runtime binding")
	}
	creativeOutputs := func() ([]applyOutput, error) {
		if cfg.DefectsEnabled {
			return defectOutputs()
		}
		if cfg.PortraitDetail {
			return c.Store.Outputs(ctx, l, "portrait.detail", "working_image")
		}
		if cfg.BeautyEnabled {
			return c.Store.Outputs(ctx, l, "portrait.beauty", "working_image")
		}
		return c.Store.Outputs(ctx, l, "image.local", "working_image")
	}
	var prev []applyOutput
	previousReview := ""
	if l.GraphVersion == 4 && l.StageIndex != nil && *l.StageIndex > 0 && *l.StageIndex < len(v4Plan) && v4Plan[*l.StageIndex-1].Cardinality == "review" && v4Plan[*l.StageIndex-1].Phase != "cull_review" {
		previousReview = v4Plan[*l.StageIndex-1].Phase
		if previousReview == "defects_review" {
			prev, e = defectOutputs()
		} else {
			prev, e = c.Store.CandidateOutputs(ctx, l, previousReview)
		}
	} else if l.Phase == "stats" {
		prev, e = c.Store.Outputs(ctx, l, "mask.person", "person_mask_ref")
	} else if l.Phase == "resolve" && (adaptive || applyStyleCullingEnabled(effectiveStyle)) {
		prev, e = c.Store.Outputs(ctx, l, "color.stats", "stats")
	} else if l.Phase == "color" {
		if cfg.GeometryEnabled && l.GraphVersion == 4 {
			prev, e = c.Store.Outputs(ctx, l, "geometry.body", "working_image")
		} else if denoiseEnabled {
			prev, e = c.Store.Outputs(ctx, l, "image.denoise", "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
		}
	} else if l.Phase == "pose" {
		if denoiseEnabled {
			prev, e = c.Store.Outputs(ctx, l, "image.denoise", "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
		}
	} else if l.Phase == "denoise" {
		prev, e = c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
	} else if l.Phase == "geometry" {
		prev, e = c.Store.Outputs(ctx, l, "pose.detect", "pose_set")
	} else if l.Phase == "mask_transform" {
		prev, e = c.Store.Outputs(ctx, l, "geometry.body", "working_image")
	} else if l.Phase == "series_measure" {
		prev, e = c.Store.Outputs(ctx, l, "image.color", "working_image")
	} else if l.Phase == "series_balance" {
		if applyUsesDirectSeries(l) {
			prev, e = c.Store.Outputs(ctx, l, "series.measure", "series_stats")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "series.pack", "series_stats_pack")
		}
	} else if l.Phase == "local" {
		if dustEnabled {
			dustOp := "sensor.dust_repair"
			if autoDust {
				dustOp = "sensor.dust_auto_repair"
			}
			prev, e = c.Store.Outputs(ctx, l, dustOp, "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "image.color", "working_image")
		}
	} else if l.Phase == "face_detect" {
		prev, e = c.Store.Outputs(ctx, l, "image.local", "working_image")
	} else if l.Phase == "face_landmarks" {
		prev, e = c.Store.Outputs(ctx, l, "face.detect", "face_boxes")
	} else if l.Phase == "face_parse" {
		prev, e = c.Store.Outputs(ctx, l, "face.landmarks", "face_landmarks")
	} else if l.Phase == "face_skin" {
		prev, e = c.Store.Outputs(ctx, l, "face.parse", "face_labels_ref")
	} else if l.Phase == "beauty" {
		prev, e = c.Store.Outputs(ctx, l, "mask.face_skin", "skin_mask_ref")
	} else if l.Phase == "portrait_detail" {
		if cfg.BeautyEnabled {
			prev, e = c.Store.Outputs(ctx, l, "portrait.beauty", "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "image.local", "working_image")
		}
	} else if l.Phase == "defects_auto_repair" {
		prev, e = c.Store.Outputs(ctx, l, "defects.propose", "defect_candidates")
	} else if l.Phase == "defects_propose" {
		prev, e = c.Store.Outputs(ctx, l, "mask.face_skin", "skin_mask_ref")
	} else if l.Phase == "depth" {
		prev, e = creativeOutputs()
	} else if l.Phase == "background_inspect" {
		prev, e = creativeOutputs()
	} else if l.Phase == "background_decode" {
		prev, e = c.Store.Outputs(ctx, l, "asset.inspect", "metadata")
	} else if l.Phase == "background" {
		if backgroundMode == "blur" {
			prev, e = c.Store.Outputs(ctx, l, "depth.predict", "relative_depth_ref")
		} else {
			prev, e = creativeOutputs()
		}
	} else if l.Phase == "composition_structure" {
		if backgroundEnabled {
			prev, e = c.Store.Outputs(ctx, l, backgroundOp, "working_image")
		} else {
			prev, e = creativeOutputs()
		}
	} else if l.Phase == "composition" {
		prev, e = c.Store.Outputs(ctx, l, "composition.structure", "image_structure")
	} else if l.Phase == "composition_render" {
		prev, e = c.Store.Outputs(ctx, l, "composition.plan", "composition_plan")
	} else if l.Phase == "inpaint" {
		if backgroundEnabled {
			prev, e = c.Store.Outputs(ctx, l, backgroundOp, "working_image")
		} else {
			prev, e = creativeOutputs()
		}
	} else if l.Phase == "generate" {
		if cfg.StyleConfig.Extensions.Inpainting.Enabled {
			prev, e = c.Store.CandidateOutputs(ctx, l, "inpaint_review")
		} else if backgroundEnabled {
			prev, e = c.Store.Outputs(ctx, l, backgroundOp, "working_image")
		} else {
			prev, e = creativeOutputs()
		}
	} else if l.Phase == "upscale" {
		if backgroundEnabled {
			prev, e = c.Store.Outputs(ctx, l, backgroundOp, "working_image")
		} else if cfg.DefectsEnabled {
			prev, e = defectOutputs()
		} else if cfg.PortraitDetail {
			prev, e = c.Store.Outputs(ctx, l, "portrait.detail", "working_image")
		} else if cfg.BeautyEnabled {
			prev, e = c.Store.Outputs(ctx, l, "portrait.beauty", "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "image.local", "working_image")
		}
	} else if l.Phase == "quality" {
		if upscaleEnabled {
			prev, e = c.Store.Outputs(ctx, l, "image.upscale", "working_image")
		} else if compositionEnabled {
			prev, e = c.Store.Outputs(ctx, l, "geometry.crop", "working_image")
		} else if backgroundEnabled {
			prev, e = c.Store.Outputs(ctx, l, backgroundOp, "working_image")
		} else if cfg.DefectsEnabled {
			prev, e = defectOutputs()
		} else if cfg.PortraitDetail {
			prev, e = c.Store.Outputs(ctx, l, "portrait.detail", "working_image")
		} else if cfg.BeautyEnabled {
			prev, e = c.Store.Outputs(ctx, l, "portrait.beauty", "working_image")
		} else {
			prev, e = c.Store.Outputs(ctx, l, "image.local", "working_image")
		}
	}
	if e != nil {
		return false, e
	}
	if l.Phase != "series_balance" && l.Phase != "mask" && !(l.Phase == "resolve" && !adaptive) && len(prev) != len(items) {
		return false, errors.New("apply predecessor count mismatch")
	}
	byItem := map[uuid.UUID]applyOutput{}
	for _, v := range prev {
		byItem[v.ItemID] = v
	}
	if l.Phase == "series_balance" {
		if !cfg.SeriesEnabled || !applyUsesDirectSeries(l) && len(prev) != (len(items)+79)/80 || applyUsesDirectSeries(l) && (len(prev) != len(items) || len(prev) > 128) {
			return false, errors.New("invalid series aggregate cardinality")
		}
		if ok, er := c.Store.Renew(ctx, l); er != nil || !ok {
			if er == nil {
				er = errors.New("apply lease lost")
			}
			return false, er
		}
		inputs := make([]review.ArtifactRef, len(prev))
		prereq := make([]uuid.UUID, len(prev))
		for i, output := range prev {
			inputs[i] = output.Artifact.ref()
			prereq[i] = output.TaskID
		}
		params, _ := canonical.Marshal(struct {
			Scene      string               `json:"scene_id"`
			Stats      []review.ArtifactRef `json:"stats_batches"`
			Reference  *uuid.UUID           `json:"reference_item_id"`
			EV         float64              `json:"ev_limit"`
			GainMin    float64              `json:"gain_min"`
			GainMax    float64              `json:"gain_max"`
			Confidence float64              `json:"min_confidence"`
			Algorithm  string               `json:"algorithm"`
		}{"collection:" + l.SourceCollectionID.String(), inputs, nil, .35, .95, 1.05, .75, "seriesmatch_v1"})
		if applyUsesDirectSeries(l) {
			var legacy map[string]json.RawMessage
			_ = json.Unmarshal(params, &legacy)
			legacy["stats"] = legacy["stats_batches"]
			delete(legacy, "stats_batches")
			params, _ = canonical.Marshal(legacy)
		}
		item := items[0]
		key := "series.balance/0000"
		id := nodeID(l.RunID, key)
		sum := sha256.Sum256(params)
		plan := applyNodePlan{id, l.Phase, 0, item.ItemID, item.AssetID, item.VersionID, key, "series.balance", inputs, prereq, hex.EncodeToString(sum[:]), params}
		ok, er := c.Store.Register(ctx, l, plan)
		if er != nil || !ok {
			return false, errors.New("apply series aggregate registration rejected")
		}
		ref, er := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: id, Purpose: "task_params"}, params)
		if er != nil {
			return false, er
		}
		profile, er := applySeriesBalanceProfile(int64(len(params)),
			cfg.SeriesModelLock, cfg.SeriesRuntimeLock,
			cfg.SeriesDeviceProfile)
		if er != nil {
			return false, er
		}
		if !applyUsesDirectSeries(l) {
			profile, er = applySeriesAggregateResources(profile, prev, int64(len(params)))
		}
		if er != nil {
			return false, er
		}
		task, er := prepareApplyMaterialization(reg, plan, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}, profile)
		if er != nil {
			return false, er
		}
		ok, er = c.Store.Materialize(ctx, l, task)
		if er != nil || !ok {
			return false, errors.New("apply series aggregate materialization rejected")
		}
		ok, er = c.Store.Advance(ctx, l, "")
		if er != nil || !ok {
			return false, errors.New("apply series aggregate advance rejected")
		}
		return true, nil
	}
	var qualityCompositionAdopted []applyOutput
	if l.Phase == "quality" && compositionEnabled && !upscaleEnabled {
		qualityCompositionAdopted, e = c.Store.CandidateOutputs(ctx, l, "composition_review")
		if e != nil {
			return false, e
		}
	}

	for _, item := range items {
		activeMaskItem = item.ItemID
		overrideMasks.MaskRefs = globalMaskBindings
		if masks, exists := itemMaskBindings[item.ItemID]; exists {
			overrideMasks.MaskRefs = masks
		}
		if ok, er := c.Store.Renew(ctx, l); er != nil || !ok {
			if er == nil {
				er = errors.New("apply lease lost")
			}
			return false, er
		}
		var params []byte
		var inputs []review.ArtifactRef
		var prereq []uuid.UUID
		op := map[string]string{"mask": "mask.person", "stats": "color.stats", "resolve": "style.resolve", "denoise": "image.denoise", "pose": "pose.detect", "geometry": "geometry.body", "mask_transform": "mask.transform", "color": "image.color", "series_measure": "series.measure", "local": "image.local", "face_detect": "face.detect", "face_landmarks": "face.landmarks", "face_parse": "face.parse", "face_skin": "mask.face_skin", "beauty": "portrait.beauty", "portrait_detail": "portrait.detail", "defects_propose": "defects.propose", "defects_auto_repair": "defects.auto_repair", "depth": "depth.predict", "background": backgroundOp, "background_inspect": "asset.inspect", "background_decode": "asset.decode", "inpaint": "image.inpaint", "generate": "image.generate", "composition_structure": "composition.structure", "composition": "composition.plan", "composition_render": "geometry.crop", "upscale": "image.upscale", "quality": "quality.check"}[l.Phase]
		manifest := item.Image.ref()
		p := byItem[item.ItemID]
		var cropProfile json.RawMessage
		var backgroundProfile json.RawMessage
		legacySizing := applyLegacySizing{Adaptive: adaptive, Alpha: item.Working.Alpha != nil}
		profilePixels, profileBytes := item.Working.Canvas.Width*item.Working.Canvas.Height, item.Image.Bytes
		switch l.Phase {
		case "mask":
			if !cfg.PersonRequired || cfg.PersonBackend == "" {
				return false, errors.New("person mask phase not authorized")
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				Long       int                `json:"long_side"`
				Tile       int                `json:"guided_tile"`
			}{manifest, "lraspp_mobilenet_v3_large", cfg.PersonLock, "person_guided_v1", 768, 512})
			inputs = refsForImage(manifest, item.Working)
		case "stats":
			raw, e2 := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if e2 != nil {
				return false, e2
			}
			mask, e2 := review.ParseMaskRef(raw)
			if e2 != nil || validateApplyPersonMask(mask, item.Working, cfg.PersonLock) != nil {
				return false, errors.New("invalid person mask binding")
			}
			params, _ = canonical.Marshal(struct {
				Image  review.ArtifactRef `json:"image"`
				Person review.MaskRef     `json:"person"`
			}{manifest, mask})
			inputs = append(refsForImage(manifest, item.Working), mask.Artifact)
			prereq = []uuid.UUID{p.TaskID}
		case "resolve":
			if applyStyleCullingEnabled(effectiveStyle) {
				prereq = []uuid.UUID{p.TaskID}
			}
			var stats *review.ArtifactRef
			if adaptive {
				v := p.Artifact.ref()
				stats = &v
				prereq = []uuid.UUID{p.TaskID}
			}
			var style *review.ArtifactRef
			if item.Style != nil {
				v := item.Style.ref()
				style = &v
			}
			resolvedOverride := cfg.OverrideConfig
			if cfg.AnalysisDecisionSHA != "" {
				resolvedOverride = itemOverride[item.ItemID]
			}
			resolveJobOverride, resolveErr := applyResolveJobOverride(cfg.Strength, cfg.JobOverrides)
			if resolveErr != nil {
				return false, resolveErr
			}
			params, _ = canonical.Marshal(struct {
				Style       *review.ArtifactRef `json:"style,omitempty"`
				Identity    bool                `json:"identity"`
				Image       review.ArtifactRef  `json:"image"`
				Stats       *review.ArtifactRef `json:"stats"`
				Strict      bool                `json:"strict_ood"`
				Group       *string             `json:"group_prototype_id"`
				Override    json.RawMessage     `json:"override,omitempty"`
				JobOverride json.RawMessage     `json:"job_override,omitempty"`
			}{style, cfg.Identity, manifest, stats, false, nil, resolvedOverride, resolveJobOverride})
			inputs = []review.ArtifactRef{manifest}
			if style != nil {
				inputs = append(inputs, *style)
			}
			if stats != nil {
				inputs = append(inputs, *stats)
				inputs = append(inputs, item.Working.Pixels)
				if item.Working.Alpha != nil {
					inputs = append(inputs, *item.Working.Alpha)
				}
			}
		case "denoise":
			if !denoiseEnabled || cfg.StyleConfig.Extensions.Restoration.DenoiseStrength < 0 || cfg.StyleConfig.Extensions.Restoration.DenoiseStrength > 1 {
				return false, errors.New("denoise phase not authorized")
			}
			params, _ = canonical.Marshal(struct {
				Image    review.ArtifactRef `json:"image"`
				Strength float64            `json:"strength"`
				Model    string             `json:"model_id"`
				Lock     string             `json:"model_lock_id"`
				Adapter  string             `json:"color_adapter_id"`
				Tile     int                `json:"tile"`
				Overlap  int                `json:"overlap"`
			}{manifest, cfg.StyleConfig.Extensions.Restoration.DenoiseStrength, "scunet_color_real_psnr", cfg.RestorationLock, "work_srgb_delta_v1", 512, 64})
			inputs = refsForImage(manifest, item.Working)
			prereq = []uuid.UUID{p.TaskID}
		case "color":
			resolvedOutput := p
			imageRef := manifest
			imageWorking := item.Working
			if cfg.GeometryEnabled && l.GraphVersion == 4 {
				imageRef = p.Artifact.ref()
				imageWorking, e = readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
				if e != nil {
					return false, e
				}
				resolvedAll, er := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
				if er != nil || len(resolvedAll) != len(items) {
					return false, errors.New("geometry resolved binding count mismatch")
				}
				for _, x := range resolvedAll {
					if x.ItemID == item.ItemID {
						resolvedOutput = x
					}
				}
			} else if denoiseEnabled {
				imageRef = p.Artifact.ref()
				imageWorking, e = readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
				if e != nil {
					return false, e
				}
				resolvedAll, er := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
				if er != nil || len(resolvedAll) != len(items) {
					return false, errors.New("denoise resolved binding count mismatch")
				}
				for _, x := range resolvedAll {
					if x.ItemID == item.ItemID {
						resolvedOutput = x
					}
				}
			}
			resolvedBytes, resourceErr := readApplyJSON(ctx, c.Objects, c.Bucket, resolvedOutput.Artifact)
			if resourceErr != nil {
				return false, resourceErr
			}
			var resolvedResources struct {
				LUT *struct {
					Size int `json:"size"`
				} `json:"absolute_lut"`
			}
			if json.Unmarshal(resolvedBytes, &resolvedResources) != nil {
				return false, errors.New("color resource binding invalid")
			}
			if resolvedResources.LUT != nil {
				legacySizing.LUTSize = resolvedResources.LUT.Size
			}
			params, _ = canonical.Marshal(struct {
				Image    review.ArtifactRef `json:"image"`
				Resolved review.ArtifactRef `json:"resolved"`
			}{imageRef, resolvedOutput.Artifact.ref()})
			inputs = append(refsForImage(imageRef, imageWorking), resolvedOutput.Artifact.ref())
			prereq = []uuid.UUID{p.TaskID}
			if resolvedOutput.TaskID != p.TaskID {
				prereq = append(prereq, resolvedOutput.TaskID)
			}
		case "pose":
			poseRef, poseWorking := manifest, item.Working
			if denoiseEnabled {
				poseRef = p.Artifact.ref()
				poseWorking, e = readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
				if e != nil {
					return false, e
				}
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				Min        int                `json:"min_size"`
				Max        int                `json:"max_size"`
				Threshold  float64            `json:"score_threshold"`
				People     int                `json:"max_people"`
			}{poseRef, "keypointrcnn_resnet50_fpn", cfg.PersonLock, "keypointrcnn_coco_v1", 640, 1024, .75, 32})
			inputs = refsForImage(poseRef, poseWorking)
			if !denoiseEnabled {
				inputs = append(inputs, p.Artifact.ref())
			}
			prereq = []uuid.UUID{p.TaskID}
		case "geometry":
			geometryRef, geometryWorking := manifest, item.Working
			if denoiseEnabled {
				denoised, er := c.Store.Outputs(ctx, l, "image.denoise", "working_image")
				if er != nil || len(denoised) != len(items) {
					return false, errors.New("geometry denoise binding count mismatch")
				}
				for _, x := range denoised {
					if x.ItemID == item.ItemID {
						geometryRef = x.Artifact.ref()
						geometryWorking, er = readWorking(ctx, c.Objects, c.Bucket, x.Artifact)
						if er != nil {
							return false, er
						}
					}
				}
			}
			masks, er := c.Store.Outputs(ctx, l, "mask.person", "person_mask_ref")
			if er != nil || len(masks) != len(items) {
				return false, errors.New("geometry person mask count mismatch")
			}
			resolvedAll, er := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
			if er != nil || len(resolvedAll) != len(items) {
				return false, errors.New("geometry resolved count mismatch")
			}
			var mask review.MaskRef
			var maskOut, resolvedOut applyOutput
			for _, x := range masks {
				if x.ItemID == item.ItemID {
					maskOut = x
					raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, x.Artifact)
					if er != nil {
						return false, er
					}
					mask, er = review.ParseMaskRef(raw)
					if er != nil {
						return false, er
					}
				}
			}
			for _, x := range resolvedAll {
				if x.ItemID == item.ItemID {
					resolvedOut = x
				}
			}
			raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, resolvedOut.Artifact)
			if er != nil {
				return false, er
			}
			var resolved struct {
				Strength float64 `json:"strength"`
				Modules  struct {
					Geometry struct {
						Enabled bool    `json:"enabled"`
						Body    float64 `json:"body_strength"`
						Face    float64 `json:"face_slim"`
						Waist   float64 `json:"waist_slim"`
						Hip     float64 `json:"hip_slim"`
						Torso   float64 `json:"torso_slim"`
						Leg     float64 `json:"leg_length"`
						Max     float64 `json:"max_displacement_image_fraction"`
					} `json:"geometry"`
				} `json:"modules"`
			}
			if json.Unmarshal(raw, &resolved) != nil || !resolved.Modules.Geometry.Enabled {
				return false, errors.New("invalid resolved geometry")
			}
			params, _ = canonical.Marshal(struct {
				Image        review.ArtifactRef `json:"image"`
				Poses        review.ArtifactRef `json:"poses"`
				Person       review.MaskRef     `json:"person"`
				Coefficients struct {
					Face  float64 `json:"face_slim"`
					Waist float64 `json:"waist_slim"`
					Hip   float64 `json:"hip_slim"`
					Torso float64 `json:"torso_slim"`
					Leg   float64 `json:"leg_length"`
				} `json:"coefficients"`
				Strength  float64 `json:"strength"`
				Algorithm string  `json:"algorithm"`
				Protect   bool    `json:"protect_background"`
				Max       float64 `json:"max_displacement_image_fraction"`
				Unsafe    string  `json:"on_unsafe"`
			}{geometryRef, p.Artifact.ref(), mask, struct {
				Face  float64 `json:"face_slim"`
				Waist float64 `json:"waist_slim"`
				Hip   float64 `json:"hip_slim"`
				Torso float64 `json:"torso_slim"`
				Leg   float64 `json:"leg_length"`
			}{resolved.Modules.Geometry.Face, resolved.Modules.Geometry.Waist, resolved.Modules.Geometry.Hip, resolved.Modules.Geometry.Torso, resolved.Modules.Geometry.Leg}, resolved.Strength * resolved.Modules.Geometry.Body, "bodywarp_v1", true, resolved.Modules.Geometry.Max, "skip_module"})
			inputs = append(refsForImage(geometryRef, geometryWorking), p.Artifact.ref(), mask.Artifact, resolvedOut.Artifact.ref())
			prereq = []uuid.UUID{p.TaskID, maskOut.TaskID, resolvedOut.TaskID}
		case "mask_transform":
			target, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			step, er := review.LastTransformStep(target)
			if er != nil {
				return false, er
			}
			masks, er := c.Store.Outputs(ctx, l, "mask.person", "person_mask_ref")
			if er != nil || len(masks) != len(items) {
				return false, errors.New("transform source mask count mismatch")
			}
			var mask review.MaskRef
			var maskTask uuid.UUID
			for _, x := range masks {
				if x.ItemID == item.ItemID {
					raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, x.Artifact)
					if er != nil {
						return false, er
					}
					mask, er = review.ParseMaskRef(raw)
					if er != nil {
						return false, er
					}
					maskTask = x.TaskID
				}
			}
			regionRules, regionErr := applyStyleRegionRules(effectiveStyle)
			if regionErr != nil {
				return false, regionErr
			}
			regionSources, regionErr := explicitApplyRegionSources(regionRules, overrideMasks.MaskRefs, mask)
			if regionErr != nil {
				return false, regionErr
			}
			params, _ = canonical.Marshal(struct {
				Mask    review.MaskRef            `json:"mask"`
				Target  review.ArtifactRef        `json:"target"`
				Step    json.RawMessage           `json:"step"`
				DType   string                    `json:"dtype"`
				Regions []applyLocalRegionBinding `json:"regions,omitempty"`
			}{mask, p.Artifact.ref(), step, "float32", regionSources})
			inputs = append(refsForImage(p.Artifact.ref(), target), mask.Artifact)
			for _, region := range regionSources {
				found := false
				for _, ref := range inputs {
					if ref == region.Mask.Artifact {
						found = true
						break
					}
				}
				if !found {
					inputs = append(inputs, region.Mask.Artifact)
				}
			}
			var stepDoc struct {
				InverseField *review.ArtifactRef `json:"inverse_field"`
			}
			_ = json.Unmarshal(step, &stepDoc)
			if stepDoc.InverseField != nil {
				inputs = append(inputs, *stepDoc.InverseField)
			}
			prereq = []uuid.UUID{p.TaskID, maskTask}
		case "series_measure":
			if !cfg.SeriesEnabled || !cfg.PersonRequired {
				return false, errors.New("series measurement not authorized")
			}
			colored, e2 := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if e2 != nil {
				return false, e2
			}
			maskOp, maskRole := "mask.person", "person_mask_ref"
			if cfg.GeometryEnabled {
				maskOp, maskRole = "mask.transform", "mask_transform_result"
			}
			masks, e2 := c.Store.Outputs(ctx, l, maskOp, maskRole)
			if e2 != nil || len(masks) != len(items) {
				return false, errors.New("series person mask count mismatch")
			}
			var mask review.MaskRef
			var maskTask uuid.UUID
			for _, m := range masks {
				if m.ItemID == item.ItemID {
					raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, m.Artifact)
					if er != nil {
						return false, er
					}
					if cfg.GeometryEnabled {
						mask, er = review.ParseTransformedMask(raw)
					} else {
						mask, er = review.ParseMaskRef(raw)
					}
					if er != nil || mask.Selector != "person" || mask.ModelLockID == nil || *mask.ModelLockID != cfg.PersonLock {
						return false, errors.New("invalid series mask binding")
					}
					maskTask = m.TaskID
				}
			}
			if maskTask == uuid.Nil || colored.Canvas != mask.Canvas || !bytes.Equal(colored.TransformChain, mask.TransformChain) {
				return false, errors.New("series mask geometry mismatch")
			}
			params, _ = canonical.Marshal(struct {
				Item    uuid.UUID          `json:"item_id"`
				Image   review.ArtifactRef `json:"image"`
				Regions []struct {
					ID   string         `json:"region_id"`
					Mask review.MaskRef `json:"mask"`
				} `json:"regions"`
				Algorithm string `json:"algorithm"`
			}{item.ItemID, p.Artifact.ref(), []struct {
				ID   string         `json:"region_id"`
				Mask review.MaskRef `json:"mask"`
			}{{"person", mask}}, "seriesmatch_v1"})
			inputs = append(refsForImage(p.Artifact.ref(), colored), mask.Artifact)
			prereq = []uuid.UUID{p.TaskID, maskTask}
		case "local":
			colored, e2 := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if e2 != nil {
				return false, e2
			}
			baseRef, baseWorking := manifest, item.Working
			var baseTask uuid.UUID
			if cfg.GeometryEnabled {
				geometry, er := c.Store.Outputs(ctx, l, "geometry.body", "working_image")
				if er != nil || len(geometry) != len(items) {
					return false, errors.New("geometry base binding count mismatch")
				}
				for _, x := range geometry {
					if x.ItemID == item.ItemID {
						baseRef = x.Artifact.ref()
						baseTask = x.TaskID
						var er error
						baseWorking, er = readWorking(ctx, c.Objects, c.Bucket, x.Artifact)
						if er != nil {
							return false, er
						}
					}
				}
			} else if denoiseEnabled {
				denoised, er := c.Store.Outputs(ctx, l, "image.denoise", "working_image")
				if er != nil || len(denoised) != len(items) {
					return false, errors.New("local denoise base missing")
				}
				for _, x := range denoised {
					if x.ItemID == item.ItemID {
						baseRef = x.Artifact.ref()
						baseTask = x.TaskID
						baseWorking, er = readWorking(ctx, c.Objects, c.Bucket, x.Artifact)
						if er != nil {
							return false, er
						}
					}
				}
			}
			if colored.Canvas != baseWorking.Canvas || !bytes.Equal(colored.TransformChain, baseWorking.TransformChain) {
				return false, errors.New("colored image geometry changed")
			}
			resolved, e2 := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
			if e2 != nil || len(resolved) != len(items) {
				return false, errors.New("resolved binding count mismatch")
			}
			var rr applyOutput
			for _, x := range resolved {
				if x.ItemID == item.ItemID {
					rr = x
				}
			}
			var person any
			var transformedRegionalRoot []byte
			var personRef *review.ArtifactRef
			var personTask uuid.UUID
			if cfg.PersonRequired {
				maskOp, maskRole := "mask.person", "person_mask_ref"
				if cfg.GeometryEnabled {
					maskOp, maskRole = "mask.transform", "mask_transform_result"
				}
				masks, e3 := c.Store.Outputs(ctx, l, maskOp, maskRole)
				if e3 != nil || len(masks) != len(items) {
					return false, errors.New("person mask count mismatch")
				}
				for _, m := range masks {
					if m.ItemID == item.ItemID {
						raw, e3 := readApplyJSON(ctx, c.Objects, c.Bucket, m.Artifact)
						if e3 != nil {
							return false, e3
						}
						var bound review.MaskRef
						if cfg.GeometryEnabled {
							transformedRegionalRoot = raw
							bound, e3 = review.ParseTransformedMask(raw)
						} else {
							bound, e3 = review.ParseMaskRef(raw)
						}
						if e3 != nil {
							return false, e3
						}
						if bound.Selector != "person" || bound.ModelLockID == nil || *bound.ModelLockID != cfg.PersonLock || bound.Canvas != baseWorking.Canvas || !bytes.Equal(bound.TransformChain, baseWorking.TransformChain) {
							return false, errors.New("invalid local person mask binding")
						}
						person = bound
						v := bound.Artifact
						personRef = &v
						personTask = m.TaskID
					}
				}
				if personRef == nil {
					return false, errors.New("person mask binding missing")
				}
			}
			var seriesRef *review.ArtifactRef
			var seriesTask uuid.UUID
			if cfg.SeriesEnabled {
				series, er := c.Store.Outputs(ctx, l, "series.balance", "series_corrections")
				if er != nil || len(series) != 1 {
					return false, errors.New("series correction binding missing")
				}
				v := series[0].Artifact.ref()
				seriesRef = &v
				seriesTask = series[0].TaskID
			}
			resolvedRaw, regionErr := readApplyJSON(ctx, c.Objects, c.Bucket, rr.Artifact)
			if regionErr != nil {
				return false, regionErr
			}
			regionRules, regionErr := resolvedApplyRegionRules(resolvedRaw)
			if regionErr != nil {
				return false, regionErr
			}
			var regionalPerson *review.MaskRef
			if bound, ok := person.(review.MaskRef); ok {
				regionalPerson = &bound
			}
			regionalMasks := overrideMasks.MaskRefs
			if cfg.GeometryEnabled {
				regionalMasks, regionErr = transformedApplyRegionBindings(transformedRegionalRoot, regionalMasks, baseRef, baseWorking)
				if regionErr != nil {
					return false, regionErr
				}
			}
			regionalBindings, regionErr := bindApplyLocalRegions(regionRules, regionalPerson, regionalMasks, baseWorking)
			if regionErr != nil {
				return false, regionErr
			}
			var resolvedLocal struct {
				Local struct {
					Enabled bool `json:"enabled"`
				} `json:"local"`
			}
			if json.Unmarshal(resolvedRaw, &resolvedLocal) != nil {
				return false, errors.New("local resource binding invalid")
			}
			if bound, ok := person.(review.MaskRef); ok && resolvedLocal.Local.Enabled && bound.Confidence >= .75 {
				legacySizing.Masks++
			}
			for _, binding := range regionalBindings {
				for _, rule := range regionRules {
					expected := rule.Selector
					if binding.Invert {
						expected = "person"
					}
					if binding.ID == rule.ID && binding.Mask.Selector == expected && binding.Mask.Confidence >= .75 {
						legacySizing.Masks++
						break
					}
				}
			}
			params, _ = canonical.Marshal(struct {
				Item     *uuid.UUID                `json:"item_id,omitempty"`
				Image    review.ArtifactRef        `json:"image"`
				Base     review.ArtifactRef        `json:"base_aligned"`
				Resolved review.ArtifactRef        `json:"resolved"`
				Series   *review.ArtifactRef       `json:"series,omitempty"`
				Person   any                       `json:"person"`
				Regions  []applyLocalRegionBinding `json:"regions"`
			}{func() *uuid.UUID {
				if seriesRef != nil {
					return &item.ItemID
				}
				return nil
			}(), p.Artifact.ref(), baseRef, rr.Artifact.ref(), seriesRef, person, regionalBindings})
			inputs = append(refsForImage(p.Artifact.ref(), colored), refsForImage(baseRef, baseWorking)...)
			inputs = append(inputs, rr.Artifact.ref())
			prereq = []uuid.UUID{p.TaskID, rr.TaskID}
			if baseTask != uuid.Nil {
				prereq = append(prereq, baseTask)
			}
			if personRef != nil {
				inputs = append(inputs, *personRef)
				prereq = append(prereq, personTask)
			}
			for _, region := range regionalBindings {
				found := false
				for _, ref := range inputs {
					if ref == region.Mask.Artifact {
						found = true
						break
					}
				}
				if !found {
					inputs = append(inputs, region.Mask.Artifact)
				}
			}
			if seriesRef != nil {
				inputs = append(inputs, *seriesRef)
				prereq = append(prereq, seriesTask)
			}
			uniqueInputs := make([]review.ArtifactRef, 0, len(inputs))
			seenInputs := map[review.ArtifactRef]bool{}
			for _, ref := range inputs {
				if !seenInputs[ref] {
					uniqueInputs = append(uniqueInputs, ref)
					seenInputs[ref] = true
				}
			}
			inputs = uniqueInputs
			if len(inputs) > 128 {
				return false, errors.New("local input limit exceeded")
			}
		case "face_detect":
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				Long       int                `json:"long_side"`
				Score      float64            `json:"score_threshold"`
				NMS        float64            `json:"nms_iou"`
				Max        int                `json:"max_faces"`
			}{p.Artifact.ref(), "sfd", cfg.PersonLock, "sfd_long1024_bgr_v1", 1024, .8, .3, 64})
			inputs = refsForImage(p.Artifact.ref(), working)
			prereq = []uuid.UUID{p.TaskID}
		case "face_landmarks":
			imageOut, er := c.Store.Outputs(ctx, l, "image.local", "working_image")
			if er != nil || len(imageOut) != len(items) {
				return false, errors.New("face landmark image binding count mismatch")
			}
			var image applyOutput
			for _, candidate := range imageOut {
				if candidate.ItemID == item.ItemID {
					image = candidate
				}
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, image.Artifact)
			if er != nil {
				return false, er
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Faces      review.ArtifactRef `json:"faces"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				Crop       int                `json:"crop_size"`
				Expansion  float64            `json:"expansion"`
			}{image.Artifact.ref(), p.Artifact.ref(), "fan2d", cfg.PersonLock, "fan2d_crop256_v1", 256, 1.25})
			inputs = append(refsForImage(image.Artifact.ref(), working), p.Artifact.ref())
			prereq = []uuid.UUID{p.TaskID, image.TaskID}
		case "face_parse":
			boxes, er := c.Store.Outputs(ctx, l, "face.detect", "face_boxes")
			if er != nil || len(boxes) != len(items) {
				return false, errors.New("face parse boxes binding count mismatch")
			}
			images, er := c.Store.Outputs(ctx, l, "image.local", "working_image")
			if er != nil || len(images) != len(items) {
				return false, errors.New("face parse image binding count mismatch")
			}
			var boxesOut, imageOut applyOutput
			for _, candidate := range boxes {
				if candidate.ItemID == item.ItemID {
					boxesOut = candidate
				}
			}
			for _, candidate := range images {
				if candidate.ItemID == item.ItemID {
					imageOut = candidate
				}
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, imageOut.Artifact)
			if er != nil {
				return false, er
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Faces      review.ArtifactRef `json:"faces"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				Crop       int                `json:"crop_size"`
				Expansion  float64            `json:"expansion"`
			}{imageOut.Artifact.ref(), boxesOut.Artifact.ref(), "bisenet_face19", cfg.PersonLock, "bisenet_face19_crop512_v1", 512, 1.35})
			inputs = append(refsForImage(imageOut.Artifact.ref(), working), boxesOut.Artifact.ref())
			prereq = []uuid.UUID{p.TaskID, boxesOut.TaskID, imageOut.TaskID}
		case "face_skin":
			raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			var labels struct {
				Labels review.ArtifactRef `json:"labels"`
			}
			if json.Unmarshal(raw, &labels) != nil || labels.Labels.ID == uuid.Nil {
				return false, errors.New("invalid face labels binding")
			}
			params, _ = canonical.Marshal(struct {
				Labels    review.ArtifactRef `json:"labels"`
				Algorithm string             `json:"algorithm"`
			}{p.Artifact.ref(), "bisenet_face19_skin_v1"})
			inputs = []review.ArtifactRef{p.Artifact.ref(), labels.Labels}
			prereq = []uuid.UUID{p.TaskID}
		case "beauty":
			if !cfg.BeautyEnabled {
				return false, errors.New("beauty phase not authorized")
			}
			skinRaw, er := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			skin, er := review.ParseMaskRef(skinRaw)
			if er != nil || skin.Selector != "skin" || skin.Protected {
				return false, errors.New("invalid beauty skin binding")
			}
			images, er := c.Store.Outputs(ctx, l, "image.local", "working_image")
			if er != nil || len(images) != len(items) {
				return false, errors.New("beauty image binding count mismatch")
			}
			landmarks, er := c.Store.Outputs(ctx, l, "face.landmarks", "face_landmarks")
			if er != nil || len(landmarks) != len(items) {
				return false, errors.New("beauty landmark binding count mismatch")
			}
			resolved, er := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
			if er != nil || len(resolved) != len(items) {
				return false, errors.New("beauty resolved binding count mismatch")
			}
			var imageOut, landmarkOut, resolvedOut applyOutput
			for _, candidate := range images {
				if candidate.ItemID == item.ItemID {
					imageOut = candidate
				}
			}
			for _, candidate := range landmarks {
				if candidate.ItemID == item.ItemID {
					landmarkOut = candidate
				}
			}
			for _, candidate := range resolved {
				if candidate.ItemID == item.ItemID {
					resolvedOut = candidate
				}
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, imageOut.Artifact)
			if er != nil {
				return false, er
			}
			if skin.Canvas != working.Canvas || !bytes.Equal(skin.TransformChain, working.TransformChain) {
				return false, errors.New("beauty skin geometry mismatch")
			}
			landmarkRaw, er := readApplyJSON(ctx, c.Objects, c.Bucket, landmarkOut.Artifact)
			if er != nil {
				return false, er
			}
			var landmarkDoc struct {
				Landmarks []struct {
					ObjectID uuid.UUID `json:"object_id"`
				} `json:"landmarks"`
			}
			if json.Unmarshal(landmarkRaw, &landmarkDoc) != nil || len(landmarkDoc.Landmarks) > 64 {
				return false, errors.New("invalid beauty landmarks")
			}
			type beautyFace struct {
				Landmarks  review.ArtifactRef `json:"landmarks"`
				ObjectID   uuid.UUID          `json:"object_id"`
				Skin       review.MaskRef     `json:"skin"`
				Manual     any                `json:"manual"`
				ManualMode bool               `json:"manual_mode"`
			}
			faces := make([]beautyFace, len(landmarkDoc.Landmarks))
			for i, face := range landmarkDoc.Landmarks {
				if face.ObjectID == uuid.Nil {
					return false, errors.New("invalid beauty face identity")
				}
				faces[i] = beautyFace{landmarkOut.Artifact.ref(), face.ObjectID, skin, nil, false}
			}
			params, _ = canonical.Marshal(struct {
				Image     review.ArtifactRef `json:"image"`
				Resolved  review.ArtifactRef `json:"resolved"`
				Faces     []beautyFace       `json:"faces"`
				Permanent any                `json:"permanent"`
			}{imageOut.Artifact.ref(), resolvedOut.Artifact.ref(), faces, nil})
			inputs = append(refsForImage(imageOut.Artifact.ref(), working), resolvedOut.Artifact.ref(), landmarkOut.Artifact.ref(), skin.Artifact)
			prereq = []uuid.UUID{p.TaskID, imageOut.TaskID, landmarkOut.TaskID, resolvedOut.TaskID}
		case "portrait_detail":
			if !cfg.PortraitDetail {
				return false, errors.New("portrait detail phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			resolved, er := c.Store.Outputs(ctx, l, "style.resolve", "resolved_style")
			if er != nil || len(resolved) != len(items) {
				return false, errors.New("portrait detail resolved binding count mismatch")
			}
			var resolvedOut applyOutput
			for _, candidate := range resolved {
				if candidate.ItemID == item.ItemID {
					resolvedOut = candidate
				}
			}
			detailMasks := overrideMasks.MaskRefs
			if cfg.GeometryEnabled {
				geometry, bindErr := c.Store.Outputs(ctx, l, "geometry.body", "working_image")
				if bindErr != nil || len(geometry) != len(items) {
					return false, errors.New("portrait geometry binding count mismatch")
				}
				var geometryRef review.ArtifactRef
				for _, candidate := range geometry {
					if candidate.ItemID == item.ItemID {
						geometryRef = candidate.Artifact.ref()
					}
				}
				transforms, bindErr := c.Store.Outputs(ctx, l, "mask.transform", "mask_transform_result")
				if bindErr != nil || len(transforms) != len(items) || geometryRef.ID == uuid.Nil {
					return false, errors.New("portrait transform binding count mismatch")
				}
				var transformRoot []byte
				for _, candidate := range transforms {
					if candidate.ItemID == item.ItemID {
						transformRoot, bindErr = readApplyJSON(ctx, c.Objects, c.Bucket, candidate.Artifact)
						if bindErr != nil {
							return false, bindErr
						}
						prereq = append(prereq, candidate.TaskID)
					}
				}
				detailMasks, bindErr = transformedApplyRegionBindings(transformRoot, detailMasks, geometryRef, working)
				if bindErr != nil {
					return false, bindErr
				}
			}
			type detailRegion struct {
				RegionID  string         `json:"region_id"`
				Kind      string         `json:"kind"`
				Mask      review.MaskRef `json:"mask"`
				Confirmed bool           `json:"confirmed"`
				Strength  float64        `json:"strength"`
			}
			kinds := map[string]string{"portrait_teeth": "teeth", "portrait_sclera": "sclera", "portrait_shine": "shine", "portrait_under_eye": "under_eye", "portrait_cloth": "cloth", "portrait_glasses": "glasses"}
			regions := make([]detailRegion, 0, len(overrideMasks.MaskRefs))
			protected := make([]review.MaskRef, 0, len(overrideMasks.MaskRefs))
			seen := map[string]bool{}
			for _, binding := range detailMasks {
				if binding.Purpose != "protected" {
					if _, relevant := kinds[binding.Purpose]; !relevant {
						continue
					}
				}
				if binding.Mask.Canvas != working.Canvas || !bytes.Equal(binding.Mask.TransformChain, working.TransformChain) || binding.Strength < 0 || binding.Strength > 1 || seen[binding.BindingID] {
					return false, errors.New("invalid portrait detail mask geometry")
				}
				seen[binding.BindingID] = true
				if binding.Purpose == "protected" {
					if !binding.Mask.Protected {
						return false, errors.New("invalid portrait protection binding")
					}
					protected = append(protected, binding.Mask)
					inputs = append(inputs, binding.Mask.Artifact)
					continue
				}
				kind, ok := kinds[binding.Purpose]
				if !ok || binding.Mask.Protected {
					continue
				}
				regions = append(regions, detailRegion{binding.BindingID, kind, binding.Mask, binding.Confirmed, binding.Strength})
				inputs = append(inputs, binding.Mask.Artifact)
			}
			if len(regions) == 0 || resolvedOut.TaskID == uuid.Nil {
				return false, errors.New("portrait detail regions missing")
			}
			params, _ = canonical.Marshal(struct {
				Image     review.ArtifactRef `json:"image"`
				Resolved  review.ArtifactRef `json:"resolved"`
				Regions   []detailRegion     `json:"regions"`
				Protected []review.MaskRef   `json:"protected"`
			}{p.Artifact.ref(), resolvedOut.Artifact.ref(), regions, protected})
			inputs = append(refsForImage(p.Artifact.ref(), working), append([]review.ArtifactRef{resolvedOut.Artifact.ref()}, inputs...)...)
			prereq = append(prereq, p.TaskID, resolvedOut.TaskID)
		case "defects_auto_repair":
			if !autoDefects {
				return false, errors.New("conservative defects not authorized")
			}
			var er error
			params, inputs, prereq, profilePixels, profileBytes, er = c.conservativeDefectParams(ctx, l, p, item.ItemID)
			if er != nil {
				return false, er
			}
		case "defects_propose":
			if !cfg.DefectsEnabled {
				return false, errors.New("defects proposal phase not authorized")
			}
			skinRaw, er := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			skin, er := review.ParseMaskRef(skinRaw)
			if er != nil || skin.Selector != "skin" || skin.Protected {
				return false, errors.New("invalid defect skin binding")
			}
			imageOp := "image.local"
			if cfg.PortraitDetail {
				imageOp = "portrait.detail"
			} else if cfg.BeautyEnabled {
				imageOp = "portrait.beauty"
			}
			images, er := c.Store.Outputs(ctx, l, imageOp, "working_image")
			if er != nil || len(images) != len(items) {
				return false, errors.New("defect image binding count mismatch")
			}
			landmarks, er := c.Store.Outputs(ctx, l, "face.landmarks", "face_landmarks")
			if er != nil || len(landmarks) != len(items) {
				return false, errors.New("defect landmark binding count mismatch")
			}
			var imageOut, landmarkOut applyOutput
			for _, candidate := range images {
				if candidate.ItemID == item.ItemID {
					imageOut = candidate
				}
			}
			for _, candidate := range landmarks {
				if candidate.ItemID == item.ItemID {
					landmarkOut = candidate
				}
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, imageOut.Artifact)
			if er != nil {
				return false, er
			}
			if skin.Canvas != working.Canvas || !bytes.Equal(skin.TransformChain, working.TransformChain) {
				return false, errors.New("defect skin geometry mismatch")
			}
			landmarkRaw, er := readApplyJSON(ctx, c.Objects, c.Bucket, landmarkOut.Artifact)
			if er != nil {
				return false, er
			}
			var landmarkDoc struct {
				Landmarks []struct {
					ObjectID uuid.UUID `json:"object_id"`
				} `json:"landmarks"`
			}
			if json.Unmarshal(landmarkRaw, &landmarkDoc) != nil || len(landmarkDoc.Landmarks) > 64 {
				return false, errors.New("invalid defect landmarks")
			}
			type defectFace struct {
				Landmarks  review.ArtifactRef `json:"landmarks"`
				ObjectID   uuid.UUID          `json:"object_id"`
				Skin       review.MaskRef     `json:"skin"`
				Manual     any                `json:"manual"`
				ManualMode bool               `json:"manual_mode"`
			}
			faces := make([]defectFace, len(landmarkDoc.Landmarks))
			for i, face := range landmarkDoc.Landmarks {
				if face.ObjectID == uuid.Nil {
					return false, errors.New("invalid defect face identity")
				}
				faces[i] = defectFace{landmarkOut.Artifact.ref(), face.ObjectID, skin, nil, false}
			}
			_, defectProtected, protectErr := creativeMasks(working, "")
			if protectErr != nil {
				return false, protectErr
			}
			params, _ = canonical.Marshal(struct {
				Protections []review.MaskRef   `json:"protections,omitempty"`
				Image       review.ArtifactRef `json:"image"`
				Faces       []defectFace       `json:"faces"`
				Permanent   any                `json:"permanent"`
				Diameter    []float64          `json:"diameter_face_fraction"`
			}{defectProtected, imageOut.Artifact.ref(), faces, nil, []float64{.002, .012}})
			inputs = append(refsForImage(imageOut.Artifact.ref(), working), landmarkOut.Artifact.ref(), skin.Artifact)
			for _, mask := range defectProtected {
				inputs = append(inputs, mask.Artifact)
			}
			prereq = []uuid.UUID{p.TaskID, imageOut.TaskID, landmarkOut.TaskID}
		case "depth":
			if backgroundMode != "blur" || cfg.PersonBackend == "" || cfg.PersonLock == "" {
				return false, errors.New("depth phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			params, _ = canonical.Marshal(struct {
				Image      review.ArtifactRef `json:"image"`
				Model      string             `json:"model_id"`
				Lock       string             `json:"model_lock_id"`
				Preprocess string             `json:"preprocessing_id"`
				InputSize  int                `json:"input_size"`
			}{p.Artifact.ref(), "depth_anything_v2_small", cfg.PersonLock, "depth_anything_v2_small_518_v1", 518})
			inputs = refsForImage(p.Artifact.ref(), working)
			prereq = []uuid.UUID{p.TaskID}
		case "background_inspect":
			if backgroundMode != "asset" || cfg.BackgroundAsset == nil {
				return false, errors.New("background inspection not authorized")
			}
			pin := cfg.BackgroundAsset
			params, _ = canonical.Marshal(struct {
				Source review.ArtifactRef `json:"source"`
				Bytes  int64              `json:"expected_bytes"`
				Suffix string             `json:"suffix"`
			}{pin.Source, pin.ExpectedBytes, pin.Suffix})
			inputs = []review.ArtifactRef{pin.Source}
			prereq = []uuid.UUID{p.TaskID}
			backgroundProfile, e = applyBackgroundAssetProfile(pin, nil, int64(len(params)))
			if e != nil {
				return false, e
			}
		case "background_decode":
			if backgroundMode != "asset" || cfg.BackgroundAsset == nil {
				return false, errors.New("background decoding not authorized")
			}
			raw, readErr := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if readErr != nil {
				return false, readErr
			}
			metadata, parseErr := applyBackgroundAssetMetadata(raw, cfg.BackgroundAsset)
			if parseErr != nil {
				return false, parseErr
			}
			pin := cfg.BackgroundAsset
			params, _ = canonical.Marshal(struct {
				Source  review.ArtifactRef `json:"source"`
				Bytes   int64              `json:"expected_bytes"`
				Suffix  string             `json:"suffix"`
				Assume  *string            `json:"assume_profile"`
				Analyze bool               `json:"analyze"`
				Tile    int                `json:"tile"`
			}{pin.Source, pin.ExpectedBytes, pin.Suffix, nil, false, 512})
			inputs = []review.ArtifactRef{pin.Source}
			prereq = []uuid.UUID{p.TaskID}
			backgroundProfile, e = applyBackgroundAssetProfile(pin, &metadata, int64(len(params)))
			if e != nil {
				return false, e
			}
		case "background":
			if !backgroundEnabled || !cfg.PersonRequired || !cfg.StyleConfig.Extensions.Background.ProtectSubject {
				return false, errors.New("background phase not authorized")
			}
			imageOut := p
			var depth *applyRelativeDepth
			if backgroundMode == "blur" {
				images, loadErr := creativeOutputs()
				if loadErr != nil || len(images) != len(items) {
					return false, errors.New("background image binding count mismatch")
				}
				for _, candidate := range images {
					if candidate.ItemID == item.ItemID {
						imageOut = candidate
					}
				}
				raw, readErr := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
				if readErr != nil {
					return false, readErr
				}
				var parsed applyRelativeDepth
				if json.Unmarshal(raw, &parsed) != nil || parsed.Artifact.ID == uuid.Nil || parsed.Normalization != "minmax_valid" || parsed.InvalidMask != nil {
					return false, errors.New("invalid background depth binding")
				}
				depth = &parsed
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, imageOut.Artifact)
			if er != nil {
				return false, er
			}
			maskOp, maskRole := "mask.person", "person_mask_ref"
			if cfg.GeometryEnabled {
				maskOp, maskRole = "mask.transform", "mask_transform_result"
			}
			masks, er := c.Store.Outputs(ctx, l, maskOp, maskRole)
			if er != nil || len(masks) != len(items) {
				return false, errors.New("background subject binding count mismatch")
			}
			var subject review.MaskRef
			var maskTask uuid.UUID
			for _, candidate := range masks {
				if candidate.ItemID != item.ItemID {
					continue
				}
				raw, readErr := readApplyJSON(ctx, c.Objects, c.Bucket, candidate.Artifact)
				if readErr != nil {
					return false, readErr
				}
				if cfg.GeometryEnabled {
					subject, readErr = review.ParseTransformedMask(raw)
				} else {
					subject, readErr = review.ParseMaskRef(raw)
				}
				if readErr != nil {
					return false, readErr
				}
				maskTask = candidate.TaskID
			}
			if maskTask == uuid.Nil || subject.Selector != "person" || subject.Protected || subject.Canvas != working.Canvas || !bytes.Equal(subject.TransformChain, working.TransformChain) {
				return false, errors.New("invalid background subject binding")
			}
			if backgroundMode == "generate" {
				target, protected, maskErr := creativeMasks(working, "generation_target")
				if maskErr != nil {
					return false, maskErr
				}
				subject.Protected = true
				protected = append(protected, subject)
				g := cfg.StyleConfig.Extensions.Generation
				if !g.Enabled || len([]rune(g.Prompt)) > 2000 || len([]rune(g.Negative)) > 2000 || g.Steps < 1 || g.Steps > 50 || g.Guidance < 0 || g.Guidance > 20 || g.Strength < 0 || g.Strength > 1 {
					return false, errors.New("invalid background generation config")
				}
				params, _ = canonical.Marshal(struct {
					Image     review.ArtifactRef `json:"image"`
					Mask      review.MaskRef     `json:"mask"`
					Protected []review.MaskRef   `json:"protected"`
					Confirmed bool               `json:"confirmed"`
					Prompt    string             `json:"prompt"`
					Negative  string             `json:"negative_prompt"`
					Seed      uint32             `json:"seed"`
					Steps     int                `json:"steps"`
					Guidance  float64            `json:"guidance_scale"`
					Model     string             `json:"model_id"`
					Lock      string             `json:"model_lock_id"`
					Feather   int                `json:"feather_pixels"`
					Strength  float64            `json:"strength"`
				}{imageOut.Artifact.ref(), target, protected, true, g.Prompt, g.Negative, g.Seed, g.Steps, g.Guidance, "sd15_inpaint", cfg.CreativeLock, 8, g.Strength})
				inputs = append(refsForImage(imageOut.Artifact.ref(), working), target.Artifact, subject.Artifact)
				for _, m := range protected {
					if m.Artifact != subject.Artifact {
						inputs = append(inputs, m.Artifact)
					}
				}
				prereq = []uuid.UUID{imageOut.TaskID, maskTask}
				break
			}
			mode := backgroundMode
			solid := cfg.StyleConfig.Extensions.Background.SolidRGB
			if mode == "solid" && len(solid) != 3 || mode == "transparent" && len(solid) != 0 || mode == "blur" && (len(solid) != 0 || cfg.StyleConfig.Extensions.Background.BlurStrength < 0 || cfg.StyleConfig.Extensions.Background.BlurStrength > 1) {
				return false, errors.New("invalid background color binding")
			}
			if mode == "blur" {
				if depth.Canvas != working.Canvas || !bytes.Equal(depth.TransformChain, working.TransformChain) {
					return false, errors.New("background depth geometry mismatch")
				}
				params, _ = canonical.Marshal(struct {
					Image    review.ArtifactRef `json:"image"`
					Subject  review.MaskRef     `json:"subject"`
					Depth    applyRelativeDepth `json:"depth"`
					Strength float64            `json:"strength"`
					Tile     int                `json:"tile"`
				}{imageOut.Artifact.ref(), subject, *depth, cfg.StyleConfig.Extensions.Background.BlurStrength, 512})
				inputs = append(refsForImage(imageOut.Artifact.ref(), working), subject.Artifact, p.Artifact.ref(), depth.Artifact)
				prereq = []uuid.UUID{p.TaskID, imageOut.TaskID, maskTask}
				break
			}
			var backgroundRef *review.ArtifactRef
			var backgroundInputs []review.ArtifactRef
			var backgroundTask uuid.UUID
			if mode == "asset" {
				decoded, loadErr := c.Store.Outputs(ctx, l, "asset.decode", "working_image")
				if loadErr != nil || len(decoded) != len(items) {
					return false, errors.New("background decode output count mismatch")
				}
				for _, candidate := range decoded {
					if candidate.ItemID == item.ItemID {
						bg, readErr := readWorking(ctx, c.Objects, c.Bucket, candidate.Artifact)
						if readErr != nil {
							return false, readErr
						}
						if readErr = validateApplyDecodedBackground(cfg.BackgroundAsset, bg, working); readErr != nil {
							return false, readErr
						}
						ref := candidate.Artifact.ref()
						backgroundRef = &ref
						backgroundInputs = refsForImage(ref, bg)
						backgroundTask = candidate.TaskID
					}
				}
				if backgroundRef == nil {
					return false, errors.New("background item binding missing")
				}
			}
			params, _ = canonical.Marshal(struct {
				Image         review.ArtifactRef `json:"image"`
				Subject       review.MaskRef     `json:"subject"`
				Mode          string             `json:"mode"`
				SolidRGB      []float64          `json:"solid_rgb,omitempty"`
				Background    any                `json:"background"`
				AssetScale    float64            `json:"asset_scale"`
				PositionX     float64            `json:"position_x"`
				PositionY     float64            `json:"position_y"`
				ShadowOpacity float64            `json:"shadow_opacity"`
				ShadowBlur    float64            `json:"shadow_blur_fraction"`
				ShadowOffsetX float64            `json:"shadow_offset_x"`
				ShadowOffsetY float64            `json:"shadow_offset_y"`
				Tile          int                `json:"tile"`
			}{imageOut.Artifact.ref(), subject, mode, solid, backgroundRef, 1, .5, .5, 0, 0, 0, 0, 512})
			inputs = append(refsForImage(imageOut.Artifact.ref(), working), subject.Artifact)
			prereq = []uuid.UUID{imageOut.TaskID, maskTask}
			inputs = append(inputs, backgroundInputs...)
			if backgroundTask != uuid.Nil {
				prereq = append(prereq, backgroundTask)
			}
		case "inpaint":
			if !cfg.StyleConfig.Extensions.Inpainting.Enabled {
				return false, errors.New("inpaint phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			target, protected, maskErr := creativeMasks(working, "inpaint_target")
			if maskErr != nil {
				return false, maskErr
			}
			params, _ = canonical.Marshal(struct {
				Image     review.ArtifactRef `json:"image"`
				Mask      review.MaskRef     `json:"mask"`
				Protected []review.MaskRef   `json:"protected"`
				Confirmed bool               `json:"confirmed"`
				Model     string             `json:"model_id"`
				Lock      string             `json:"model_lock_id"`
				Context   int                `json:"context_pixels"`
				Feather   int                `json:"feather_pixels"`
			}{p.Artifact.ref(), target, protected, true, "big_lama", cfg.CreativeLock, 128, 8})
			inputs = append(refsForImage(p.Artifact.ref(), working), target.Artifact)
			for _, m := range protected {
				inputs = append(inputs, m.Artifact)
			}
			prereq = []uuid.UUID{p.TaskID}
		case "generate":
			g := cfg.StyleConfig.Extensions.Generation
			if !g.Enabled || len([]rune(g.Prompt)) > 2000 || len([]rune(g.Negative)) > 2000 || g.Steps < 1 || g.Steps > 50 || g.Guidance < 0 || g.Guidance > 20 || g.Strength < 0 || g.Strength > 1 {
				return false, errors.New("generation phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			target, protected, maskErr := creativeMasks(working, "generation_target")
			if maskErr != nil {
				return false, maskErr
			}
			params, _ = canonical.Marshal(struct {
				Image     review.ArtifactRef `json:"image"`
				Mask      review.MaskRef     `json:"mask"`
				Protected []review.MaskRef   `json:"protected"`
				Confirmed bool               `json:"confirmed"`
				Prompt    string             `json:"prompt"`
				Negative  string             `json:"negative_prompt"`
				Seed      uint32             `json:"seed"`
				Steps     int                `json:"steps"`
				Guidance  float64            `json:"guidance_scale"`
				Model     string             `json:"model_id"`
				Lock      string             `json:"model_lock_id"`
				Feather   int                `json:"feather_pixels"`
				Strength  float64            `json:"strength"`
			}{p.Artifact.ref(), target, protected, true, g.Prompt, g.Negative, g.Seed, g.Steps, g.Guidance, "sd15_inpaint", cfg.CreativeLock, 8, g.Strength})
			inputs = append(refsForImage(p.Artifact.ref(), working), target.Artifact)
			for _, m := range protected {
				inputs = append(inputs, m.Artifact)
			}
			prereq = []uuid.UUID{p.TaskID}
		case "composition_structure":
			if !compositionEnabled || len(cfg.StyleConfig.Extensions.Composition.Aspect) != 2 {
				return false, errors.New("composition structure phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			var subject *review.MaskRef
			var maskTask uuid.UUID
			maskOp, maskRole := "mask.person", "person_mask_ref"
			if cfg.GeometryEnabled {
				maskOp, maskRole = "mask.transform", "mask_transform_result"
			}
			masks, er := c.Store.Outputs(ctx, l, maskOp, maskRole)
			if er != nil || len(masks) != len(items) {
				return false, errors.New("composition subject mask missing")
			}
			for _, m := range masks {
				if m.ItemID == item.ItemID {
					raw, readErr := readApplyJSON(ctx, c.Objects, c.Bucket, m.Artifact)
					if readErr != nil {
						return false, readErr
					}
					var parsed review.MaskRef
					if cfg.GeometryEnabled {
						parsed, readErr = review.ParseTransformedMask(raw)
					} else {
						parsed, readErr = review.ParseMaskRef(raw)
					}
					if readErr != nil {
						return false, readErr
					}
					subject = &parsed
					maskTask = m.TaskID
				}
			}
			if subject == nil || subject.Canvas != working.Canvas || !bytes.Equal(subject.TransformChain, working.TransformChain) {
				return false, errors.New("composition subject geometry mismatch")
			}
			params, _ = canonical.Marshal(struct {
				Image    review.ArtifactRef `json:"image"`
				Subject  *review.MaskRef    `json:"subject"`
				MaxLines int                `json:"max_lines"`
				Low      int                `json:"canny_low"`
				High     int                `json:"canny_high"`
			}{p.Artifact.ref(), subject, 512, 50, 150})
			inputs = append(refsForImage(p.Artifact.ref(), working), subject.Artifact)
			prereq = []uuid.UUID{p.TaskID, maskTask}
		case "composition":
			comp := cfg.StyleConfig.Extensions.Composition
			if !compositionEnabled || len(comp.Aspect) != 2 || comp.Aspect[0] < 1 || comp.Aspect[1] < 1 || comp.Headroom < .01 || comp.Headroom > .5 || comp.Rotate < 0 || comp.Rotate > 5 {
				return false, errors.New("invalid composition config")
			}
			raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			var structure struct {
				Image review.ArtifactRef `json:"image"`
			}
			if json.Unmarshal(raw, &structure) != nil || structure.Image.ID == uuid.Nil {
				return false, errors.New("invalid image structure")
			}
			params, _ = canonical.Marshal(struct {
				Structure    review.ArtifactRef `json:"structure"`
				AspectWidth  int                `json:"aspect_width"`
				AspectHeight int                `json:"aspect_height"`
				Headroom     float64            `json:"headroom_target"`
				Allow        bool               `json:"allow_rotation"`
				MaxRotation  float64            `json:"max_rotation_degrees"`
				Perspective  float64            `json:"max_perspective_area_change"`
			}{p.Artifact.ref(), comp.Aspect[0], comp.Aspect[1], comp.Headroom, comp.Rotate > 0, comp.Rotate, .15})
			inputs = []review.ArtifactRef{p.Artifact.ref(), structure.Image}
			prereq = []uuid.UUID{p.TaskID}
		case "composition_render":
			raw, er := readApplyJSON(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			var plan struct {
				Image        review.ArtifactRef `json:"image"`
				Disposition  string             `json:"disposition"`
				Side         *string            `json:"outpaint_side"`
				Pixels       *int               `json:"outpaint_pixels"`
				OutputWidth  int                `json:"output_width"`
				OutputHeight int                `json:"output_height"`
			}
			if json.Unmarshal(raw, &plan) != nil || plan.Image.ID == uuid.Nil || plan.OutputWidth < 1 || plan.OutputHeight < 1 || plan.OutputWidth > 200000000 || plan.OutputHeight > 200000000 || int64(plan.OutputWidth)*int64(plan.OutputHeight) > 200000000 {
				return false, errors.New("invalid composition render plan")
			}
			imageArtifact, er := c.Store.Artifact(ctx, l, plan.Image)
			if er != nil {
				return false, er
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, imageArtifact)
			if er != nil {
				return false, er
			}
			if plan.Disposition == "outpaint" {
				comp := cfg.StyleConfig.Extensions.Composition
				if comp.OutpaintPolicy != "review" || comp.OutpaintPrompt == "" || comp.OutpaintGuidance < 1 || comp.OutpaintGuidance > 20 || plan.Side == nil || plan.Pixels == nil || *plan.Pixels < 8 || *plan.Pixels > 8192 {
					return false, errors.New("composition outpaint not explicitly authorized")
				}
				op = "image.outpaint"
				params, _ = canonical.Marshal(struct {
					Image    review.ArtifactRef `json:"image"`
					Side     string             `json:"side"`
					Pixels   int                `json:"pixels"`
					Prompt   string             `json:"prompt"`
					Negative string             `json:"negative_prompt"`
					Seed     uint32             `json:"seed"`
					Steps    int                `json:"steps"`
					Guidance float64            `json:"guidance_scale"`
					Model    string             `json:"model_id"`
					Lock     string             `json:"model_lock_id"`
					Feather  int                `json:"feather_pixels"`
				}{plan.Image, *plan.Side, *plan.Pixels, comp.OutpaintPrompt, comp.OutpaintNegative, comp.OutpaintSeed, 25, comp.OutpaintGuidance, "sd15_inpaint", cfg.CreativeLock, 16})
				inputs = refsForImage(plan.Image, working)
				profilePixels, profileBytes = int64(plan.OutputWidth)*int64(plan.OutputHeight), imageArtifact.Bytes
			} else if plan.Disposition == "apply" || plan.Disposition == "review" {
				op = "geometry.crop"
				cropProfile, er = applyCropProfile(working.Canvas.Width*working.Canvas.Height, int64(plan.OutputWidth)*int64(plan.OutputHeight), imageArtifact.Bytes, working.Alpha != nil)
				if er != nil {
					return false, er
				}
				params, _ = canonical.Marshal(struct {
					Image         review.ArtifactRef `json:"image"`
					Plan          review.ArtifactRef `json:"plan"`
					Interpolation string             `json:"interpolation"`
				}{plan.Image, p.Artifact.ref(), "lanczos4"})
				inputs = append(refsForImage(plan.Image, working), p.Artifact.ref())
			} else {
				return false, errors.New("invalid composition disposition")
			}
			prereq = []uuid.UUID{p.TaskID}
		case "upscale":
			if !upscaleEnabled {
				return false, errors.New("upscale phase not authorized")
			}
			working, er := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if er != nil {
				return false, er
			}
			scale := 2
			model := "realesrgan_x2"
			if cfg.StyleConfig.Extensions.Restoration.Upscale == "4x" {
				scale = 4
				model = "realesrgan_x4"
			}
			params, _ = canonical.Marshal(struct {
				Image   review.ArtifactRef `json:"image"`
				Scale   int                `json:"scale"`
				Model   string             `json:"model_id"`
				Lock    string             `json:"model_lock_id"`
				Adapter string             `json:"color_adapter_id"`
				Tile    int                `json:"tile"`
				Overlap int                `json:"overlap"`
			}{p.Artifact.ref(), scale, model, cfg.RestorationLock, "work_srgb_delta_v1", 256, 32})
			inputs = refsForImage(p.Artifact.ref(), working)
			prereq = []uuid.UUID{p.TaskID}
		case "quality":
			finalImage, e2 := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if e2 != nil {
				return false, e2
			}
			baselineRef, baselineWorking := manifest, item.Working
			var baseline *review.ArtifactRef
			if cfg.GeometryEnabled && !compositionEnabled && !upscaleEnabled {
				geometry, er := c.Store.Outputs(ctx, l, "geometry.body", "working_image")
				if er != nil || len(geometry) != len(items) {
					return false, errors.New("quality geometry baseline missing")
				}
				for _, x := range geometry {
					if x.ItemID == item.ItemID {
						baselineRef = x.Artifact.ref()
						baselineWorking, er = readWorking(ctx, c.Objects, c.Bucket, x.Artifact)
						if er != nil {
							return false, er
						}
					}
				}
			}
			// Composition changes coordinates. Its approved candidate/base is a
			// provenance anchor, never a pixel reference to itself. MAE/PSNR
			// against the uncropped source are not defined in this geometry.
			if compositionEnabled && !upscaleEnabled {
				if er := validateApplyCompositionQualityTarget(item.ItemID, p.Artifact.ref(), qualityCompositionAdopted, len(items)); er != nil {
					return false, er
				}
			}
			if !upscaleEnabled && !compositionEnabled && (finalImage.Canvas != baselineWorking.Canvas || !bytes.Equal(finalImage.TransformChain, baselineWorking.TransformChain)) {
				return false, errors.New("quality image geometry changed")
			}
			if !upscaleEnabled && !compositionEnabled {
				baseline = &baselineRef
			}
			legacySizing.Baseline = baseline != nil
			legacySizing.Alpha = finalImage.Alpha != nil
			params, _ = canonical.Marshal(struct {
				Target             review.ArtifactRef  `json:"target"`
				TargetKind         string              `json:"target_kind"`
				Stage              string              `json:"stage"`
				ThresholdProfileID string              `json:"threshold_profile_id"`
				Baseline           *review.ArtifactRef `json:"baseline"`
				Protected          []any               `json:"protected"`
				Seams              []any               `json:"seams"`
			}{p.Artifact.ref(), "working", "master", "master_v1", baseline, []any{}, []any{}})
			inputs = refsForImage(p.Artifact.ref(), finalImage)
			if baseline != nil {
				inputs = append(inputs, refsForImage(baselineRef, baselineWorking)...)
			}
			prereq = []uuid.UUID{p.TaskID}
		}
		key := fmt.Sprintf("%s/%04d", op, item.Ordinal)
		id := nodeID(l.RunID, key)
		sum := sha256.Sum256(params)
		plan := applyNodePlan{id, l.Phase, item.Ordinal, item.ItemID, item.AssetID, item.VersionID, key, op, inputs, prereq, hex.EncodeToString(sum[:]), params}
		ok, e2 := c.Store.Register(ctx, l, plan)
		if e2 != nil || !ok {
			return false, errors.New("apply node registration rejected")
		}
		ref, e2 := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: id, Purpose: "task_params"}, params)
		if e2 != nil {
			return false, e2
		}
		var profile json.RawMessage
		if l.Phase == "mask" || l.Phase == "pose" || l.Phase == "face_detect" || l.Phase == "face_landmarks" || l.Phase == "face_parse" || l.Phase == "depth" {
			profile, e2 = applyModelProfile(cfg.PersonBackend, cfg.PersonLock, item.Working.Canvas.Width*item.Working.Canvas.Height, item.Image.Bytes)
		} else if l.Phase == "denoise" {
			profile, e2 = applyModelProfile(cfg.RestorationBackend, cfg.RestorationLock, item.Working.Canvas.Width*item.Working.Canvas.Height, item.Image.Bytes)
		} else if l.Phase == "upscale" {
			scale := 2
			if cfg.StyleConfig.Extensions.Restoration.Upscale == "4x" {
				scale = 4
			}
			working, profileErr := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if profileErr != nil {
				return false, profileErr
			}
			profile, e2 = applyUpscaleProfile(cfg.RestorationBackend, cfg.RestorationLock, working.Canvas.Width*working.Canvas.Height, p.Artifact.Bytes, scale, working.Alpha != nil)
		} else if l.Phase == "background_inspect" || l.Phase == "background_decode" {
			profile = backgroundProfile
		} else if l.Phase == "background" && backgroundMode != "generate" {
			working, profileErr := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if profileErr != nil {
				return false, profileErr
			}
			profile, e2 = applyBackgroundProfile(working.Canvas.Width*working.Canvas.Height, p.Artifact.Bytes, cfg.StyleConfig.Extensions.Background.Mode == "transparent")
		} else if l.Phase == "composition_render" && op == "geometry.crop" {
			profile = cropProfile
		} else if l.Phase == "composition_render" && op == "image.outpaint" {
			profile, e2 = applyCreativeProfile(cfg.CreativeBackend, cfg.CreativeLock, profilePixels, profileBytes)
		} else if l.Phase == "background" || l.Phase == "inpaint" || l.Phase == "generate" {
			working, profileErr := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if profileErr != nil {
				return false, profileErr
			}
			profile, e2 = applyCreativeProfile(cfg.CreativeBackend, cfg.CreativeLock, working.Canvas.Width*working.Canvas.Height, p.Artifact.Bytes)
		} else if l.Phase == "geometry" {
			profile, e2 = applyBodyProfile(item.Working.Canvas.Width*item.Working.Canvas.Height, item.Image.Bytes, item.Working.Alpha != nil)
		} else if l.Phase == "mask_transform" {
			profile, e2 = applyRegionTransformProfile(item.Working.Canvas.Width*item.Working.Canvas.Height, p.Artifact.Bytes, params)
		} else if l.Phase == "defects_auto_repair" {
			profile, e2 = applyConservativeProfile(profilePixels, profileBytes)
		} else if l.Phase == "quality" {
			working, profileErr := readWorking(ctx, c.Objects, c.Bucket, p.Artifact)
			if profileErr != nil {
				return false, profileErr
			}
			profile, e2 = applyProfile(working.Canvas.Width*working.Canvas.Height, p.Artifact.Bytes)
		} else {
			profile, e2 = applyProfile(item.Working.Canvas.Width*item.Working.Canvas.Height, item.Image.Bytes)
		}
		if e2 != nil {
			return false, e2
		}
		if profile, e2 = applyLegacyResourceProfile(op, profile, legacySizing); e2 != nil {
			return false, e2
		}
		if profile, e2 = c.qualifyMixedProfile(items[0].Config, l.TenantID, op, profile); e2 != nil {
			return false, e2
		}
		task, e2 := prepareApplyMaterialization(reg, plan, review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}, profile)
		if e2 != nil {
			return false, e2
		}
		ok, e2 = c.Store.Materialize(ctx, l, task)
		if e2 != nil || !ok {
			return false, errors.New("apply task materialization rejected")
		}
	}
	next := map[string]string{"mask": "stats", "stats": "resolve", "resolve": "color", "color": "local", "local": "quality", "quality": "adopt"}[l.Phase]
	if l.GraphVersion == 4 {
		next = ""
		for i, stage := range v4Plan {
			if stage.Phase == l.Phase {
				if i+1 < len(v4Plan) {
					next = v4Plan[i+1].Phase
				} else {
					next = "adopt"
				}
				break
			}
		}
	}
	if next == "" {
		return false, errors.New("apply phase has no successor")
	}
	ok, e := c.Store.Advance(ctx, l, next)
	if e != nil || !ok {
		return false, errors.New("apply phase advance rejected")
	}
	return true, nil
}

var _ artifactjson.Repository = ApplyDocuments{}
