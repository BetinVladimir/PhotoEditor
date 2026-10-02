package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type StyleAnalysisLease struct {
	TenantID, RunID, StyleID, Token uuid.UUID
	Phase                           string
	PairCount                       int
	GraphVersion                    int
	Until                           time.Time
}

type styleAnalysisDB interface {
	artifactjson.RowQuerier
	Query(context.Context, string, ...any) (pgx.Rows, error)
}
type StyleAnalysisStore struct{ DB styleAnalysisDB }

func (s StyleAnalysisStore) Claim(ctx context.Context) (*StyleAnalysisLease, error) {
	if s.DB == nil {
		return nil, errors.New("style analysis database required")
	}
	v := new(StyleAnalysisLease)
	err := s.DB.QueryRow(ctx, `SELECT c.tenant_id,c.run_id,c.style_id,c.phase,c.pair_count,c.lease_token,c.lease_until,b.graph_version FROM photo.claim_style_analysis_build() c JOIN photo.style_analysis_builds b ON b.tenant_id=c.tenant_id AND b.run_id=c.run_id`).Scan(&v.TenantID, &v.RunID, &v.StyleID, &v.Phase, &v.PairCount, &v.Token, &v.Until, &v.GraphVersion)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if v.TenantID == uuid.Nil || v.RunID == uuid.Nil || v.StyleID == uuid.Nil || v.Token == uuid.Nil || v.PairCount < 1 || v.PairCount > 32 || v.Until.IsZero() || v.GraphVersion < 1 || v.GraphVersion > 3 {
		return nil, errors.New("invalid style analysis lease")
	}
	return v, nil
}

type styleArtifact struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"key"`
	VersionID  string    `json:"version_id"`
	Bytes      int64     `json:"bytes"`
	MediaType  string    `json:"media_type"`
}

func (a styleArtifact) ref() review.ArtifactRef {
	return review.ArtifactRef{ID: a.ArtifactID, SHA256: a.SHA256}
}
func (a styleArtifact) object() objectstore.Object {
	return objectstore.Object{Key: a.Key, VersionID: a.VersionID, SHA256: a.SHA256, Bytes: a.Bytes, ContentType: a.MediaType}
}
func (a styleArtifact) valid() bool {
	_, e := domain.ParseDigest(a.SHA256)
	return a.ArtifactID != uuid.Nil && e == nil && a.Bucket != "" && a.Key != "" && a.VersionID != "" && a.VersionID != "null" && a.Bytes > 1 && a.MediaType == "application/json"
}

type stylePairInput struct {
	Ordinal       int
	PairID        uuid.UUID
	SessionID     string
	Before, After styleArtifact
	IgnoreMask    *styleArtifact
}
type styleAnalysisConfig struct {
	Schema         string    `json:"schema"`
	StyleID        uuid.UUID `json:"style_id"`
	SnapshotDigest string    `json:"snapshot_digest"`
	Options        struct {
		AlignmentMode   string `json:"alignment_mode"`
		GeometryEnabled bool   `json:"geometry_enabled"`
		Seed            uint32 `json:"seed"`
	} `json:"analysis_options"`
	FitBackend             string     `json:"fit_backend"`
	FitRuntimeLockID       string     `json:"fit_runtime_lock_id"`
	AggregateRuntimeLockID string     `json:"aggregate_runtime_lock_id"`
	HoldoutSnapshotID      *uuid.UUID `json:"holdout_snapshot_id"`
	HoldoutSnapshotDigest  *string    `json:"holdout_snapshot_digest"`
}
type styleIgnoreMask struct {
	Artifact    review.ArtifactRef `json:"artifact"`
	Canvas      review.Canvas      `json:"canvas"`
	Transform   json.RawMessage    `json:"transform_chain"`
	ObjectID    *uuid.UUID         `json:"object_id"`
	Selector    string             `json:"selector"`
	Confidence  float64            `json:"confidence"`
	ModelLockID *string            `json:"model_lock_id"`
	Protected   bool               `json:"protected"`
}

func (s StyleAnalysisStore) pairSet(ctx context.Context, l StyleAnalysisLease, holdout bool) ([]stylePairInput, styleAnalysisConfig, error) {
	query := `SELECT ordinal,pair_id,session_id,payload,snapshot_digest,config_snapshot FROM photo.style_analysis_pair_inputs($1,$2,$3)`
	if holdout {
		query = `SELECT ordinal,pair_id,session_id,payload,snapshot_digest,config_snapshot FROM photo.style_analysis_holdout_inputs($1,$2,$3)`
	}
	rows, err := s.DB.Query(ctx, query, l.TenantID, l.RunID, l.Token)
	if err != nil {
		return nil, styleAnalysisConfig{}, err
	}
	defer rows.Close()
	var out []stylePairInput
	var cfg styleAnalysisConfig
	var digest string
	for rows.Next() {
		var ordinal int64
		var pair uuid.UUID
		var session string
		var payload, config []byte
		var d string
		if err = rows.Scan(&ordinal, &pair, &session, &payload, &d, &config); err != nil {
			return nil, cfg, err
		}
		var p struct {
			PairID     uuid.UUID      `json:"pair_id"`
			SessionID  string         `json:"session_id"`
			Before     styleArtifact  `json:"before"`
			After      styleArtifact  `json:"after"`
			IgnoreMask *styleArtifact `json:"ignore_mask"`
		}
		if json.Unmarshal(payload, &p) != nil || json.Unmarshal(config, &cfg) != nil || ordinal < 0 || ordinal > 31 || p.PairID != pair || p.SessionID != session || !p.Before.valid() || !p.After.valid() || p.IgnoreMask != nil && !p.IgnoreMask.valid() {
			return nil, cfg, errors.New("invalid style analysis input")
		}
		if len(out) > 0 && d != digest {
			return nil, cfg, errors.New("style analysis digest changed")
		}
		digest = d
		out = append(out, stylePairInput{int(ordinal), pair, session, p.Before, p.After, p.IgnoreMask})
	}
	if err = rows.Err(); err != nil {
		return nil, cfg, err
	}
	expected := l.PairCount
	expectedDigest := cfg.SnapshotDigest
	if holdout {
		expectedDigest = ""
		if cfg.HoldoutSnapshotDigest != nil {
			expectedDigest = *cfg.HoldoutSnapshotDigest
		}
	}
	if len(out) != expected && !holdout || len(out) < 1 || len(out) > map[bool]int{false: 32, true: 64}[holdout] || cfg.Schema != "style-analysis/v1" || cfg.StyleID != l.StyleID || expectedDigest != digest {
		return nil, cfg, errors.New("invalid style analysis context")
	}
	if _, e := domain.ParseDigest(digest); e != nil {
		return nil, cfg, e
	}
	if _, e := domain.ParseDigest(cfg.FitRuntimeLockID); e != nil {
		return nil, cfg, e
	}
	if _, e := domain.ParseDigest(cfg.AggregateRuntimeLockID); e != nil {
		return nil, cfg, e
	}
	if holdout && (cfg.HoldoutSnapshotID == nil || *cfg.HoldoutSnapshotID == uuid.Nil || cfg.HoldoutSnapshotDigest == nil) {
		return nil, cfg, errors.New("invalid holdout config")
	}
	if cfg.HoldoutSnapshotDigest != nil {
		if _, e := domain.ParseDigest(*cfg.HoldoutSnapshotDigest); e != nil {
			return nil, cfg, e
		}
	}
	return out, cfg, nil
}
func (s StyleAnalysisStore) pairs(ctx context.Context, l StyleAnalysisLease) ([]stylePairInput, styleAnalysisConfig, error) {
	return s.pairSet(ctx, l, false)
}
func (s StyleAnalysisStore) holdoutPairs(ctx context.Context, l StyleAnalysisLease) ([]stylePairInput, styleAnalysisConfig, error) {
	return s.pairSet(ctx, l, true)
}

type styleStageOutput struct {
	Ordinal   int
	PairID    uuid.UUID
	SessionID string
	TaskID    uuid.UUID
	Ref       review.ArtifactRef
	Bytes     int64
	Object    styleArtifact
}

func (s StyleAnalysisStore) outputs(ctx context.Context, l StyleAnalysisLease, phase, op, role string) ([]styleStageOutput, error) {
	rows, err := s.DB.Query(ctx, `SELECT ordinal,pair_id,session_id,task_id,artifact_id,sha256,bytes FROM photo.style_analysis_stage_outputs($1,$2,$3,$4,$5,$6)`, l.TenantID, l.RunID, l.Token, phase, op, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []styleStageOutput
	for rows.Next() {
		var v styleStageOutput
		if err = rows.Scan(&v.Ordinal, &v.PairID, &v.SessionID, &v.TaskID, &v.Ref.ID, &v.Ref.SHA256, &v.Bytes,
			&v.Object.Bucket, &v.Object.Key, &v.Object.VersionID, &v.Object.MediaType); err != nil {
			return nil, err
		}
		v.Object.ArtifactID, v.Object.SHA256, v.Object.Bytes = v.Ref.ID, v.Ref.SHA256, v.Bytes
		if !validExpansionRef(v.Ref) || v.TaskID == uuid.Nil || v.Bytes < 2 {
			return nil, errors.New("invalid style stage output")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}
func (s StyleAnalysisStore) holdoutOutputs(ctx context.Context, l StyleAnalysisLease, op, role string) ([]styleStageOutput, error) {
	rows, err := s.DB.Query(ctx, `SELECT ordinal,pair_id,session_id,task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.style_analysis_holdout_outputs($1,$2,$3,$4,$5)`, l.TenantID, l.RunID, l.Token, op, role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []styleStageOutput
	for rows.Next() {
		var v styleStageOutput
		if err = rows.Scan(&v.Ordinal, &v.PairID, &v.SessionID, &v.TaskID, &v.Ref.ID, &v.Ref.SHA256, &v.Bytes, &v.Object.Bucket, &v.Object.Key, &v.Object.VersionID, &v.Object.MediaType); err != nil {
			return nil, err
		}
		v.Object.ArtifactID, v.Object.SHA256, v.Object.Bytes = v.Ref.ID, v.Ref.SHA256, v.Bytes
		if !validExpansionRef(v.Ref) || v.TaskID == uuid.Nil || !v.Object.valid() {
			return nil, errors.New("invalid holdout output")
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func (s StyleAnalysisStore) singleOutput(ctx context.Context, l StyleAnalysisLease, phase, op, role string) (styleStageOutput, error) {
	var v styleStageOutput
	err := s.DB.QueryRow(ctx, `SELECT task_id,artifact_id,sha256,bytes,bucket,object_key,object_version,media_type FROM photo.style_analysis_single_output($1,$2,$3,$4,$5,$6)`, l.TenantID, l.RunID, l.Token, phase, op, role).Scan(&v.TaskID, &v.Ref.ID, &v.Ref.SHA256, &v.Bytes, &v.Object.Bucket, &v.Object.Key, &v.Object.VersionID, &v.Object.MediaType)
	v.Object.ArtifactID, v.Object.SHA256, v.Object.Bytes = v.Ref.ID, v.Ref.SHA256, v.Bytes
	if err != nil {
		return v, err
	}
	if !validExpansionRef(v.Ref) || v.TaskID == uuid.Nil || !v.Object.valid() {
		return v, errors.New("invalid style single output")
	}
	return v, nil
}

func readMask(ctx context.Context, objects ArtifactOpener, bucket string, a styleArtifact) (review.MaskRef, error) {
	if objects == nil || bucket == "" || a.Bucket != bucket || !a.valid() || a.Bytes > 1048576 {
		return review.MaskRef{}, objectstore.ErrInvalid
	}
	rd, err := objects.Open(ctx, a.object())
	if err != nil {
		return review.MaskRef{}, err
	}
	defer rd.Close()
	raw, err := io.ReadAll(io.LimitReader(rd, 1048577))
	if err != nil {
		return review.MaskRef{}, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != a.Bytes || hex.EncodeToString(sum[:]) != a.SHA256 {
		return review.MaskRef{}, objectstore.ErrIntegrity
	}
	return review.ParseMaskRef(raw)
}

type faceLabelsManifest struct {
	Image          review.ArtifactRef `json:"image"`
	Faces          review.ArtifactRef `json:"faces"`
	Labels         review.ArtifactRef `json:"labels"`
	Canvas         review.Canvas      `json:"canvas"`
	TransformChain json.RawMessage    `json:"transform_chain"`
}

func readFaceLabels(ctx context.Context, objects ArtifactOpener, bucket string, a styleArtifact) (faceLabelsManifest, error) {
	var out faceLabelsManifest
	if objects == nil || bucket == "" || a.Bucket != bucket || !a.valid() || a.Bytes > 1048576 {
		return out, objectstore.ErrInvalid
	}
	rd, err := objects.Open(ctx, a.object())
	if err != nil {
		return out, err
	}
	defer rd.Close()
	raw, err := io.ReadAll(io.LimitReader(rd, 1048577))
	if err != nil {
		return out, err
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != a.Bytes || hex.EncodeToString(sum[:]) != a.SHA256 || json.Unmarshal(raw, &out) != nil || !validExpansionRef(out.Image) || !validExpansionRef(out.Faces) || !validExpansionRef(out.Labels) {
		return faceLabelsManifest{}, objectstore.ErrIntegrity
	}
	return out, nil
}

type styleNodePlan struct {
	ID            uuid.UUID            `json:"id"`
	Phase         string               `json:"phase"`
	Ordinal       int                  `json:"ordinal"`
	PairID        *uuid.UUID           `json:"pair_id"`
	NodeKey       string               `json:"node_key"`
	Operation     string               `json:"operation"`
	Inputs        []review.ArtifactRef `json:"inputs"`
	Prerequisites []uuid.UUID          `json:"prerequisites"`
	ParamsSHA     string               `json:"params_sha256"`
	Params        json.RawMessage      `json:"params"`
}

func (s StyleAnalysisStore) register(ctx context.Context, l StyleAnalysisLease, n styleNodePlan) (bool, error) {
	raw, e := canonical.Marshal(n)
	if e != nil {
		return false, e
	}
	var ok bool
	e = s.DB.QueryRow(ctx, `SELECT photo.register_style_analysis_node($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, raw).Scan(&ok)
	return ok, e
}
func (s StyleAnalysisStore) materialize(ctx context.Context, l StyleAnalysisLease, t PreparedTask) (bool, error) {
	raw, e := canonical.Marshal(t)
	if e != nil {
		return false, e
	}
	var ok bool
	e = s.DB.QueryRow(ctx, `SELECT photo.materialize_style_analysis_task($1,$2,$3,$4)`, l.TenantID, l.RunID, l.Token, raw).Scan(&ok)
	return ok, e
}
func (s StyleAnalysisStore) advance(ctx context.Context, l StyleAnalysisLease, to string) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.advance_style_analysis_build($1,$2,$3,$4)`, l.RunID, l.Token, l.Phase, to).Scan(&ok)
	return ok, e
}
func (s StyleAnalysisStore) fail(ctx context.Context, l StyleAnalysisLease, code string) (bool, error) {
	var ok bool
	e := s.DB.QueryRow(ctx, `SELECT photo.fail_style_analysis_build($1,$2,$3)`, l.RunID, l.Token, code).Scan(&ok)
	return ok, e
}
func (s StyleAnalysisStore) renew(ctx context.Context, l StyleAnalysisLease) (bool, error) {
	var ok bool
	err := s.DB.QueryRow(ctx, `SELECT photo.renew_style_analysis_build($1,$2)`, l.RunID, l.Token).Scan(&ok)
	return ok, err
}

// StyleAnalysisDocuments isolates style params from the broader review/export
// publisher authorization surface while retaining the same immutable object protocol.
type StyleAnalysisDocuments struct {
	DB     artifactjson.RowQuerier
	Bucket string
}

func (r StyleAnalysisDocuments) Reserve(ctx context.Context, s artifactjson.Scope, sha string, size int64) (artifactjson.Reservation, error) {
	if r.DB == nil || s.Purpose != "task_params" || len(r.Bucket) < 3 {
		return artifactjson.Reservation{}, objectstore.ErrInvalid
	}
	var outcome string
	var id, token *uuid.UUID
	var ready bool
	err := r.DB.QueryRow(ctx, `SELECT outcome,artifact_id,token,ready FROM photo.reserve_style_analysis_document($1,$2,$3,$4,$5,$6)`, s.Tenant, s.Run, s.Owner, sha, size, r.Bucket).Scan(&outcome, &id, &token, &ready)
	if err != nil {
		return artifactjson.Reservation{}, err
	}
	switch outcome {
	case "conflict":
		return artifactjson.Reservation{}, artifactjson.ErrConflict
	case "stale", "forbidden":
		return artifactjson.Reservation{}, artifactjson.ErrStale
	case "quota":
		return artifactjson.Reservation{}, artifactjson.ErrQuota
	case "created", "resumed", "replayed":
	default:
		return artifactjson.Reservation{}, objectstore.ErrIntegrity
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
func (r StyleAnalysisDocuments) Complete(ctx context.Context, s artifactjson.Scope, t artifactjson.Reservation, o objectstore.Object) (artifactjson.Ref, error) {
	var ok bool
	err := r.DB.QueryRow(ctx, `SELECT photo.complete_style_analysis_document($1,$2,$3,$4,$5,$6,$7,$8,$9)`, s.Tenant, s.Run, s.Owner, t.ArtifactID, t.Token, o.SHA256, o.Bytes, r.Bucket, o.VersionID).Scan(&ok)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	if !ok {
		return artifactjson.Ref{}, artifactjson.ErrStale
	}
	return artifactjson.Ref{ID: t.ArtifactID, SHA256: o.SHA256}, nil
}

type StyleAnalysisCoordinator struct {
	Store     StyleAnalysisStore
	Publisher ContractPublisher
	Objects   ArtifactOpener
	Bucket    string
}

func readWorking(ctx context.Context, objects ArtifactOpener, bucket string, a styleArtifact) (review.WorkingImage, error) {
	if objects == nil || bucket == "" || a.Bucket != bucket || !a.valid() || a.Bytes > 1048576 {
		return review.WorkingImage{}, objectstore.ErrInvalid
	}
	rd, e := objects.Open(ctx, a.object())
	if e != nil {
		return review.WorkingImage{}, e
	}
	defer rd.Close()
	raw, e := io.ReadAll(io.LimitReader(rd, 1048577))
	if e != nil {
		return review.WorkingImage{}, e
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != a.Bytes || hex.EncodeToString(sum[:]) != a.SHA256 {
		return review.WorkingImage{}, objectstore.ErrIntegrity
	}
	return review.ParseWorkingImage(raw)
}

func styleAnalysisProfile(backend, lock string, ram, vram, scratch, output, pixels int64) (json.RawMessage, error) {
	return canonical.Marshal(struct {
		Backend   string           `json:"backend"`
		Precision string           `json:"precision"`
		Tile      int              `json:"tile_size"`
		ModelLock string           `json:"model_lock_id"`
		Estimate  ResourceEstimate `json:"resource_estimate"`
	}{backend, "float32", 512, lock, ResourceEstimate{RAMBytes: ram, VRAMBytes: vram, ScratchBytes: scratch, OutputBytes: output, PixelCount: pixels}})
}
func nodeID(run uuid.UUID, key string) uuid.UUID { return uuid.NewSHA1(run, []byte(key)) }

func workingInputs(ref review.ArtifactRef, image review.WorkingImage) []review.ArtifactRef {
	out := []review.ArtifactRef{ref, image.Pixels}
	if image.Alpha != nil {
		out = append(out, *image.Alpha)
	}
	return out
}

func (c StyleAnalysisCoordinator) build(ctx context.Context, l StyleAnalysisLease) (bool, error) {
	pairs, cfg, err := c.Store.pairs(ctx, l)
	if err != nil {
		return false, err
	}
	if l.Phase == "holdout_features" || l.Phase == "holdout_registration" {
		pairs, cfg, err = c.Store.holdoutPairs(ctx, l)
		if err != nil {
			return false, err
		}
	}
	registry, err := LoadRegistry()
	if err != nil {
		return false, err
	}
	load := func(phase, op, role string, count int) (map[uuid.UUID][]styleStageOutput, error) {
		values, e := c.Store.outputs(ctx, l, phase, op, role)
		if e != nil {
			return nil, e
		}
		if len(values) != count {
			return nil, errors.New("style stage output count mismatch")
		}
		out := map[uuid.UUID][]styleStageOutput{}
		for _, value := range values {
			out[value.PairID] = append(out[value.PairID], value)
		}
		return out, nil
	}
	var plans []plannedStyle
	if l.Phase == "package" {
		style, e := c.Store.singleOutput(ctx, l, "validate", "style.validate", "validated_style")
		if e != nil {
			return false, e
		}
		report, e := c.Store.singleOutput(ctx, l, "validate", "style.validate", "quality_report")
		if e != nil {
			return false, e
		}
		params, _ := canonical.Marshal(struct {
			Style   review.ArtifactRef `json:"style"`
			Quality review.ArtifactRef `json:"quality_report"`
			Version string             `json:"package_version"`
		}{style.Ref, report.Ref, "style_package_v1"})
		plans = append(plans, makeStylePlan(l, registry, "package", 0, nil, "style.package", "cpu", []review.ArtifactRef{style.Ref, report.Ref}, []uuid.UUID{style.TaskID}, params, 256<<20, 0, 110<<20, 101<<20, 0, cfg.AggregateRuntimeLockID))
	} else if l.Phase == "validate" {
		holdout, holdCfg, e := c.Store.holdoutPairs(ctx, l)
		if e != nil {
			return false, e
		}
		if holdCfg.HoldoutSnapshotID == nil {
			return false, errors.New("holdout snapshot missing")
		}
		alignments, e := c.Store.holdoutOutputs(ctx, l, "pair.register", "alignment")
		if e != nil {
			return false, e
		}
		if len(alignments) != len(holdout) {
			return false, errors.New("holdout alignment count mismatch")
		}
		personOutputs, e := c.Store.holdoutOutputs(ctx, l, "mask.person", "person_mask_ref")
		if e != nil {
			return false, e
		}
		if len(personOutputs) != len(holdout) {
			return false, errors.New("holdout person mask count mismatch")
		}
		candidate, e := c.Store.singleOutput(ctx, l, "aggregate", "style.aggregate", "style_candidate")
		if e != nil {
			return false, e
		}
		byPair := map[uuid.UUID]styleStageOutput{}
		personByPair := map[uuid.UUID]styleStageOutput{}
		for _, v := range alignments {
			if _, ok := byPair[v.PairID]; ok {
				return false, errors.New("duplicate holdout alignment")
			}
			byPair[v.PairID] = v
		}
		for _, v := range personOutputs {
			if _, ok := personByPair[v.PairID]; ok {
				return false, errors.New("duplicate holdout person mask")
			}
			personByPair[v.PairID] = v
		}
		type validationPair struct {
			PairID    uuid.UUID          `json:"pair_id"`
			SessionID string             `json:"session_id"`
			Before    review.ArtifactRef `json:"before"`
			After     review.ArtifactRef `json:"after"`
			Alignment review.ArtifactRef `json:"alignment"`
			Person    *review.MaskRef    `json:"person"`
			Ignore    *review.MaskRef    `json:"ignore"`
		}
		items := make([]validationPair, 0, len(holdout))
		inputs := []review.ArtifactRef{candidate.Ref}
		prereq := []uuid.UUID{candidate.TaskID}
		pixels := int64(0)
		for _, p := range holdout {
			v, ok := byPair[p.PairID]
			if !ok {
				return false, errors.New("missing holdout alignment")
			}
			personOutput, ok := personByPair[p.PairID]
			if !ok {
				return false, errors.New("missing holdout person mask")
			}
			before, e := readWorking(ctx, c.Objects, c.Bucket, p.Before)
			if e != nil {
				return false, e
			}
			after, e := readWorking(ctx, c.Objects, c.Bucket, p.After)
			if e != nil {
				return false, e
			}
			pixels += int64(before.Canvas.Width*before.Canvas.Height + after.Canvas.Width*after.Canvas.Height)
			person, e := readMask(ctx, c.Objects, c.Bucket, personOutput.Object)
			if e != nil {
				return false, e
			}
			var ignore *review.MaskRef
			if p.IgnoreMask != nil {
				value, readErr := readMask(ctx, c.Objects, c.Bucket, *p.IgnoreMask)
				if readErr != nil {
					return false, readErr
				}
				ignore = &value
			}
			items = append(items, validationPair{p.PairID, p.SessionID, p.Before.ref(), p.After.ref(), v.Ref, &person, ignore})
			inputs = append(inputs, workingInputs(p.Before.ref(), before)...)
			inputs = append(inputs, workingInputs(p.After.ref(), after)...)
			inputs = append(inputs, v.Ref)
			inputs = append(inputs, personOutput.Ref, person.Artifact)
			if ignore != nil {
				inputs = append(inputs, p.IgnoreMask.ref(), ignore.Artifact)
			}
			prereq = append(prereq, v.TaskID, personOutput.TaskID)
		}
		sessions := make([]string, 0, len(pairs))
		for _, p := range pairs {
			sessions = append(sessions, p.SessionID)
		}
		params, _ := canonical.Marshal(struct {
			Style    review.ArtifactRef `json:"style"`
			Holdout  []validationPair   `json:"holdout"`
			Training []string           `json:"training_session_ids"`
			Seed     uint32             `json:"seed"`
			Samples  int                `json:"samples_per_pair"`
			Profile  string             `json:"threshold_profile_id"`
		}{candidate.Ref, items, sessions, cfg.Options.Seed, 65536, "style_validation_v1"})
		plans = append(plans, makeStylePlan(l, registry, "validate", 0, nil, "style.validate", "cpu", inputs, prereq, params, 1<<30, 0, pixels*20, 101<<20, pixels, cfg.AggregateRuntimeLockID))
	} else if l.Phase == "aggregate" {
		byPair, e := load("color_fit", "pair.fit.color", "color_fit", len(pairs))
		if e != nil {
			return false, e
		}
		beautyByPair := map[uuid.UUID][]styleStageOutput{}
		defectsByPair := map[uuid.UUID][]styleStageOutput{}
		if l.GraphVersion >= 3 {
			beautyByPair, e = load("effects_fit", "pair.fit.beauty", "beauty_fit", len(pairs))
			if e != nil {
				return false, e
			}
			defectsByPair, e = load("effects_fit", "pair.fit.defects", "defect_evidence", len(pairs))
			if e != nil {
				return false, e
			}
		}
		refs := make([]review.ArtifactRef, 0, len(pairs))
		beautyRefs := make([]review.ArtifactRef, 0, len(pairs))
		defectRefs := make([]review.ArtifactRef, 0, len(pairs))
		sessions := make([]string, 0, len(pairs))
		prereq := make([]uuid.UUID, 0, len(pairs))
		for _, p := range pairs {
			if len(byPair[p.PairID]) != 1 || l.GraphVersion >= 3 && (len(beautyByPair[p.PairID]) != 1 || len(defectsByPair[p.PairID]) != 1) {
				return false, errors.New("duplicate style stage output")
			}
			v := byPair[p.PairID][0]
			refs = append(refs, v.Ref)
			sessions = append(sessions, p.SessionID)
			prereq = append(prereq, v.TaskID)
			if l.GraphVersion >= 3 {
				beautyRefs = append(beautyRefs, beautyByPair[p.PairID][0].Ref)
				defectRefs = append(defectRefs, defectsByPair[p.PairID][0].Ref)
				prereq = append(prereq, beautyByPair[p.PairID][0].TaskID, defectsByPair[p.PairID][0].TaskID)
			}
		}
		models := []map[string]string{}
		if l.GraphVersion >= 3 {
			models = []map[string]string{{"id": "sfd", "weight_id": "face_alignment.s3fd-619a316812.pth"}, {"id": "fan2d", "weight_id": "face_alignment.2DFAN4-cd938726ad.pth.tar"}, {"id": "bisenet_face19", "weight_id": "zllrunning/79999_iter.pth.torchscript"}}
		}
		params, _ := canonical.Marshal(struct {
			StyleID    string               `json:"style_id"`
			ColorFits  []review.ArtifactRef `json:"color_fits"`
			SessionIDs []string             `json:"session_ids"`
			Seed       uint32               `json:"seed"`
			Manifest   string               `json:"pair_manifest_sha256"`
			Runtime    string               `json:"runtime_lock_id"`
			Models     []map[string]string  `json:"models"`
			Beauty     []review.ArtifactRef `json:"beauty_fits"`
			Defects    []review.ArtifactRef `json:"defect_evidence"`
		}{l.StyleID.String(), refs, sessions, cfg.Options.Seed, cfg.SnapshotDigest, cfg.AggregateRuntimeLockID,
			models, beautyRefs, defectRefs})
		inputs := append(append(append([]review.ArtifactRef{}, refs...), beautyRefs...), defectRefs...)
		plans = append(plans, makeStylePlan(l, registry, "aggregate", 0, nil, "style.aggregate", "cpu", inputs, prereq, params, 512<<20, 0, 64<<20, 100<<20, 0, cfg.AggregateRuntimeLockID))
	} else {
		var alignments, persons, poses, fits, warped, faces, landmarks, parsed, skins map[uuid.UUID][]styleStageOutput
		switch l.Phase {
		case "features", "body_fit":
			alignments, err = load("registration", "pair.register", "alignment", len(pairs))
		case "geometry":
			fits, err = load("body_fit", "pair.fit.body", "body_fit", len(pairs))
		case "geometry_registration":
			warped, err = load("geometry", "geometry.body", "working_image", len(pairs))
		case "color_fit":
			phase := "registration"
			if cfg.Options.GeometryEnabled && l.GraphVersion >= 2 {
				phase = "geometry_registration"
				warped, err = load("geometry", "geometry.body", "working_image", len(pairs))
			}
			if err == nil {
				alignments, err = load(phase, "pair.register", "alignment", len(pairs))
			}
		case "face_semantics":
			faces, err = load("face_detect", "face.detect", "face_boxes", len(pairs)*2)
		case "face_skin":
			parsed, err = load("face_semantics", "face.parse", "face_labels_ref", len(pairs)*2)
		case "effects_fit":
			landmarks, err = load("face_semantics", "face.landmarks", "face_landmarks", len(pairs)*2)
			if err == nil {
				skins, err = load("face_skin", "mask.face_skin", "skin_mask_ref", len(pairs)*2)
			}
			if err == nil {
				phase := "registration"
				if cfg.Options.GeometryEnabled && l.GraphVersion >= 2 {
					phase = "geometry_registration"
				}
				alignments, err = load(phase, "pair.register", "alignment", len(pairs))
			}
		}
		if err != nil {
			return false, err
		}
		if l.Phase == "body_fit" || l.Phase == "geometry" {
			persons, err = load("features", "mask.person", "person_mask_ref", len(pairs)*2)
			if err != nil {
				return false, err
			}
			poses, err = load("features", "pose.detect", "pose_set", len(pairs)*2)
			if err != nil {
				return false, err
			}
		}
		for _, p := range pairs {
			renewed, renewErr := c.Store.renew(ctx, l)
			if renewErr != nil || !renewed {
				if renewErr == nil {
					renewErr = errors.New("style analysis lease lost")
				}
				return false, renewErr
			}
			before, err := readWorking(ctx, c.Objects, c.Bucket, p.Before)
			if err != nil {
				return false, err
			}
			after, err := readWorking(ctx, c.Objects, c.Bucket, p.After)
			if err != nil {
				return false, err
			}
			pixels := before.Canvas.Width*before.Canvas.Height + after.Canvas.Width*after.Canvas.Height
			if l.Phase == "holdout_features" {
				params, _ := canonical.Marshal(struct {
					Image      review.ArtifactRef `json:"image"`
					Model      string             `json:"model_id"`
					Lock       string             `json:"model_lock_id"`
					Preprocess string             `json:"preprocessing_id"`
					Long       int                `json:"long_side"`
					Tile       int                `json:"guided_tile"`
				}{p.After.ref(), "lraspp_mobilenet_v3_large", cfg.FitRuntimeLockID, "person_guided_v1", 768, 512})
				pix := int64(after.Canvas.Width * after.Canvas.Height)
				plans = append(plans, makeStylePlan(l, registry, "holdout_features", p.Ordinal, &p.PairID, "mask.person", cfg.FitBackend, workingInputs(p.After.ref(), after), nil, params, 2<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pix*12, pix*6, pix, cfg.FitRuntimeLockID))
			} else if l.Phase == "registration" || l.Phase == "holdout_registration" {
				mode := "similarity"
				if cfg.Options.AlignmentMode == "crop_aware" {
					mode = "crop_aware"
				}
				params, _ := canonical.Marshal(struct {
					Before review.ArtifactRef `json:"before"`
					After  review.ArtifactRef `json:"after"`
					Seed   uint32             `json:"seed"`
					Mode   string             `json:"mode"`
				}{p.Before.ref(), p.After.ref(), cfg.Options.Seed, mode})
				inputs := append(workingInputs(p.Before.ref(), before), workingInputs(p.After.ref(), after)...)
				plans = append(plans, makeStylePlan(l, registry, l.Phase, p.Ordinal, &p.PairID, "pair.register", "cpu", inputs, nil, params, 2<<30, 0, int64(pixels)*8, 1<<20, int64(pixels), ""))
			} else if l.Phase == "face_detect" {
				for side, item := range []struct {
					ref   review.ArtifactRef
					image review.WorkingImage
				}{{p.Before.ref(), before}, {p.After.ref(), after}} {
					params, _ := canonical.Marshal(struct {
						Image      review.ArtifactRef `json:"image"`
						Model      string             `json:"model_id"`
						Lock       string             `json:"model_lock_id"`
						Preprocess string             `json:"preprocessing_id"`
						Long       int                `json:"long_side"`
						Threshold  float64            `json:"score_threshold"`
						NMS        float64            `json:"nms_iou"`
						Max        int                `json:"max_faces"`
					}{item.ref, "sfd", cfg.FitRuntimeLockID, "sfd_long1024_bgr_v1", 1024, .8, .3, 64})
					pix := item.image.Canvas.Width * item.image.Canvas.Height
					plans = append(plans, makeStylePlan(l, registry, "face_detect", p.Ordinal*2+side, &p.PairID, "face.detect", cfg.FitBackend, workingInputs(item.ref, item.image), nil, params, 1<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], int64(pix)*8, 1<<20, int64(pix), cfg.FitRuntimeLockID))
				}
			} else if l.Phase == "features" {
				if len(alignments[p.PairID]) != 1 {
					return false, errors.New("missing registration output")
				}
				for side, item := range []struct {
					ref   review.ArtifactRef
					image review.WorkingImage
				}{{p.Before.ref(), before}, {p.After.ref(), after}} {
					baseInputs := workingInputs(item.ref, item.image)
					prior := []uuid.UUID{alignments[p.PairID][0].TaskID}
					personParams, _ := canonical.Marshal(struct {
						Image      review.ArtifactRef `json:"image"`
						Model      string             `json:"model_id"`
						Lock       string             `json:"model_lock_id"`
						Preprocess string             `json:"preprocessing_id"`
						Long       int                `json:"long_side"`
						Tile       int                `json:"guided_tile"`
					}{item.ref, "lraspp_mobilenet_v3_large", cfg.FitRuntimeLockID, "person_guided_v1", 768, 512})
					poseParams, _ := canonical.Marshal(struct {
						Image      review.ArtifactRef `json:"image"`
						Model      string             `json:"model_id"`
						Lock       string             `json:"model_lock_id"`
						Preprocess string             `json:"preprocessing_id"`
						Min        int                `json:"min_size"`
						Max        int                `json:"max_size"`
						Threshold  float64            `json:"score_threshold"`
						People     int                `json:"max_people"`
					}{item.ref, "keypointrcnn_resnet50_fpn", cfg.FitRuntimeLockID, "keypointrcnn_coco_v1", 640, 1024, .75, 32})
					pix := item.image.Canvas.Width * item.image.Canvas.Height
					slot := p.Ordinal*4 + side
					plans = append(plans, makeStylePlan(l, registry, "features", slot, &p.PairID, "mask.person", cfg.FitBackend, baseInputs, prior, personParams, 2<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pix*12, pix*6, pix, cfg.FitRuntimeLockID))
					plans = append(plans, makeStylePlan(l, registry, "features", slot+2, &p.PairID, "pose.detect", cfg.FitBackend, baseInputs, prior, poseParams, 2<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pix*8, 1<<20, pix, cfg.FitRuntimeLockID))
				}
			} else if l.Phase == "body_fit" {
				if len(alignments[p.PairID]) != 1 || len(persons[p.PairID]) != 2 || len(poses[p.PairID]) != 2 {
					return false, errors.New("incomplete body features")
				}
				pm := map[int]styleStageOutput{}
				qm := map[int]styleStageOutput{}
				for _, v := range persons[p.PairID] {
					pm[v.Ordinal%4] = v
				}
				for _, v := range poses[p.PairID] {
					qm[v.Ordinal%4] = v
				}
				beforeMask, e := readMask(ctx, c.Objects, c.Bucket, pm[0].Object)
				if e != nil {
					return false, e
				}
				afterMask, e := readMask(ctx, c.Objects, c.Bucket, pm[1].Object)
				if e != nil {
					return false, e
				}
				params, _ := canonical.Marshal(struct {
					Pair         uuid.UUID          `json:"pair_id"`
					Before       review.ArtifactRef `json:"before"`
					After        review.ArtifactRef `json:"after"`
					Alignment    review.ArtifactRef `json:"alignment"`
					BeforePose   review.ArtifactRef `json:"before_pose"`
					AfterPose    review.ArtifactRef `json:"after_pose"`
					BeforePerson review.MaskRef     `json:"before_person"`
					AfterPerson  review.MaskRef     `json:"after_person"`
					Algorithm    string             `json:"algorithm"`
					Confidence   float64            `json:"min_confidence"`
					Evidence     string             `json:"evidence_mode"`
				}{p.PairID, p.Before.ref(), p.After.ref(), alignments[p.PairID][0].Ref, qm[2].Ref, qm[3].Ref, beforeMask, afterMask, "bodywarp_v1", .75, "fitted"})
				inputs := append(workingInputs(p.Before.ref(), before), workingInputs(p.After.ref(), after)...)
				inputs = append(inputs, alignments[p.PairID][0].Ref, qm[2].Ref, qm[3].Ref, pm[0].Ref, beforeMask.Artifact, pm[1].Ref, afterMask.Artifact)
				prior := []uuid.UUID{alignments[p.PairID][0].TaskID, pm[0].TaskID, pm[1].TaskID, qm[2].TaskID, qm[3].TaskID}
				plans = append(plans, makeStylePlan(l, registry, "body_fit", p.Ordinal, &p.PairID, "pair.fit.body", "cpu", inputs, prior, params, 2<<30, 0, pixels*10, 1<<20, pixels, cfg.AggregateRuntimeLockID))
			} else if l.Phase == "geometry" {
				if len(fits[p.PairID]) != 1 || len(persons[p.PairID]) != 2 || len(poses[p.PairID]) != 2 {
					return false, errors.New("incomplete body fit")
				}
				pm := map[int]styleStageOutput{}
				qm := map[int]styleStageOutput{}
				for _, v := range persons[p.PairID] {
					pm[v.Ordinal%4] = v
				}
				for _, v := range poses[p.PairID] {
					qm[v.Ordinal%4] = v
				}
				mask, e := readMask(ctx, c.Objects, c.Bucket, pm[0].Object)
				if e != nil {
					return false, e
				}
				params, _ := canonical.Marshal(struct {
					Image     review.ArtifactRef `json:"image"`
					Poses     review.ArtifactRef `json:"poses"`
					Person    review.MaskRef     `json:"person"`
					Fit       review.ArtifactRef `json:"fit"`
					Strength  float64            `json:"strength"`
					Algorithm string             `json:"algorithm"`
					Protect   bool               `json:"protect_background"`
					Max       float64            `json:"max_displacement_image_fraction"`
					Unsafe    string             `json:"on_unsafe"`
				}{p.Before.ref(), qm[2].Ref, mask, fits[p.PairID][0].Ref, 1, "bodywarp_v1", true, .02, "skip_module"})
				inputs := workingInputs(p.Before.ref(), before)
				inputs = append(inputs, qm[2].Ref, pm[0].Ref, mask.Artifact, fits[p.PairID][0].Ref)
				prior := []uuid.UUID{fits[p.PairID][0].TaskID}
				plans = append(plans, makeStylePlan(l, registry, "geometry", p.Ordinal, &p.PairID, "geometry.body", "cpu", inputs, prior, params, 3<<30, 0, before.Canvas.Width*before.Canvas.Height*24, before.Canvas.Width*before.Canvas.Height*20, before.Canvas.Width*before.Canvas.Height, cfg.AggregateRuntimeLockID))
			} else if l.Phase == "geometry_registration" {
				if len(warped[p.PairID]) != 1 {
					return false, errors.New("missing geometry output")
				}
				source, e := readWorking(ctx, c.Objects, c.Bucket, warped[p.PairID][0].Object)
				if e != nil {
					return false, e
				}
				params, _ := canonical.Marshal(struct {
					Before review.ArtifactRef `json:"before"`
					After  review.ArtifactRef `json:"after"`
					Seed   uint32             `json:"seed"`
					Mode   string             `json:"mode"`
				}{warped[p.PairID][0].Ref, p.After.ref(), cfg.Options.Seed, "similarity"})
				inputs := append(workingInputs(warped[p.PairID][0].Ref, source), workingInputs(p.After.ref(), after)...)
				plans = append(plans, makeStylePlan(l, registry, "geometry_registration", p.Ordinal, &p.PairID, "pair.register", "cpu", inputs, []uuid.UUID{warped[p.PairID][0].TaskID}, params, 2<<30, 0, pixels*8, 1<<20, pixels, ""))
			} else if l.Phase == "face_semantics" {
				if len(faces[p.PairID]) != 2 {
					return false, errors.New("incomplete face detections")
				}
				fm := map[int]styleStageOutput{}
				for _, v := range faces[p.PairID] {
					fm[v.Ordinal%2] = v
				}
				for side, item := range []struct {
					ref   review.ArtifactRef
					image review.WorkingImage
				}{{p.Before.ref(), before}, {p.After.ref(), after}} {
					face := fm[side]
					base := append(workingInputs(item.ref, item.image), face.Ref)
					landmarkParams, _ := canonical.Marshal(struct {
						Image      review.ArtifactRef `json:"image"`
						Faces      review.ArtifactRef `json:"faces"`
						Model      string             `json:"model_id"`
						Lock       string             `json:"model_lock_id"`
						Preprocess string             `json:"preprocessing_id"`
						Crop       int                `json:"crop_size"`
						Expansion  float64            `json:"expansion"`
					}{item.ref, face.Ref, "fan2d", cfg.FitRuntimeLockID, "fan2d_crop256_v1", 256, 1.25})
					parseParams, _ := canonical.Marshal(struct {
						Image      review.ArtifactRef `json:"image"`
						Faces      review.ArtifactRef `json:"faces"`
						Model      string             `json:"model_id"`
						Lock       string             `json:"model_lock_id"`
						Preprocess string             `json:"preprocessing_id"`
						Crop       int                `json:"crop_size"`
						Expansion  float64            `json:"expansion"`
					}{item.ref, face.Ref, "bisenet_face19", cfg.FitRuntimeLockID, "bisenet_face19_crop512_v1", 512, 1.35})
					pix := int64(item.image.Canvas.Width * item.image.Canvas.Height)
					plans = append(plans,
						makeStylePlan(l, registry, "face_semantics", p.Ordinal*4+side, &p.PairID, "face.landmarks", cfg.FitBackend, base, []uuid.UUID{face.TaskID}, landmarkParams, 1<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pix*8, 1<<20, pix, cfg.FitRuntimeLockID),
						makeStylePlan(l, registry, "face_semantics", p.Ordinal*4+side+2, &p.PairID, "face.parse", cfg.FitBackend, base, []uuid.UUID{face.TaskID}, parseParams, 2<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pix*10, pix*3, pix, cfg.FitRuntimeLockID))
				}
			} else if l.Phase == "face_skin" {
				if len(parsed[p.PairID]) != 2 {
					return false, errors.New("incomplete face parsing")
				}
				for _, v := range parsed[p.PairID] {
					manifest, e := readFaceLabels(ctx, c.Objects, c.Bucket, v.Object)
					if e != nil {
						return false, e
					}
					params, _ := canonical.Marshal(struct {
						Labels    review.ArtifactRef `json:"labels"`
						Algorithm string             `json:"algorithm"`
					}{v.Ref, "bisenet_face19_skin_v1"})
					plans = append(plans, makeStylePlan(l, registry, "face_skin", p.Ordinal*2+v.Ordinal%2, &p.PairID, "mask.face_skin", "cpu", []review.ArtifactRef{v.Ref, manifest.Labels}, []uuid.UUID{v.TaskID}, params, 256<<20, 0, int64(manifest.Canvas.Width*manifest.Canvas.Height)*4, int64(manifest.Canvas.Width*manifest.Canvas.Height)*7, int64(manifest.Canvas.Width*manifest.Canvas.Height), cfg.AggregateRuntimeLockID))
				}
			} else if l.Phase == "effects_fit" {
				if len(alignments[p.PairID]) != 1 || len(landmarks[p.PairID]) != 2 || len(skins[p.PairID]) != 2 {
					return false, errors.New("incomplete effect evidence inputs")
				}
				lm, sm := map[int]styleStageOutput{}, map[int]styleStageOutput{}
				for _, v := range landmarks[p.PairID] {
					lm[v.Ordinal%4] = v
				}
				for _, v := range skins[p.PairID] {
					sm[v.Ordinal%2] = v
				}
				skin, e := readMask(ctx, c.Objects, c.Bucket, sm[1].Object)
				if e != nil {
					return false, e
				}
				alignment := alignments[p.PairID][0]
				beautyParams, _ := canonical.Marshal(struct {
					Pair            uuid.UUID          `json:"pair_id"`
					Before          review.ArtifactRef `json:"before"`
					After           review.ArtifactRef `json:"after"`
					Alignment       review.ArtifactRef `json:"alignment"`
					BeforeLandmarks review.ArtifactRef `json:"before_landmarks"`
					AfterLandmarks  review.ArtifactRef `json:"after_landmarks"`
					Skin            review.MaskRef     `json:"skin"`
					Permanent       any                `json:"permanent"`
					Confidence      float64            `json:"min_confidence"`
					Algorithm       string             `json:"algorithm"`
				}{p.PairID, p.Before.ref(), p.After.ref(), alignment.Ref, lm[0].Ref, lm[1].Ref, skin, nil, .75, "beauty_fit_coordinate_v1"})
				defectParams, _ := canonical.Marshal(struct {
					Pair       uuid.UUID          `json:"pair_id"`
					Before     review.ArtifactRef `json:"before"`
					After      review.ArtifactRef `json:"after"`
					Alignment  review.ArtifactRef `json:"alignment"`
					Skins      []review.MaskRef   `json:"skin_masks"`
					Permanent  any                `json:"permanent"`
					Confidence float64            `json:"min_confidence"`
					Algorithm  string             `json:"algorithm"`
				}{p.PairID, p.Before.ref(), p.After.ref(), alignment.Ref, []review.MaskRef{skin}, nil, .75, "defect_evidence_v1"})
				inputs := append(workingInputs(p.Before.ref(), before), workingInputs(p.After.ref(), after)...)
				inputs = append(inputs, alignment.Ref, lm[0].Ref, lm[1].Ref, sm[1].Ref, skin.Artifact)
				prereq := []uuid.UUID{alignment.TaskID, lm[0].TaskID, lm[1].TaskID, sm[1].TaskID}
				plans = append(plans,
					makeStylePlan(l, registry, "effects_fit", p.Ordinal*2, &p.PairID, "pair.fit.beauty", "cpu", inputs, prereq, beautyParams, 1<<30, 0, int64(pixels)*16, 1<<20, int64(pixels), cfg.AggregateRuntimeLockID),
					makeStylePlan(l, registry, "effects_fit", p.Ordinal*2+1, &p.PairID, "pair.fit.defects", "cpu", inputs, prereq, defectParams, 512<<20, 0, int64(pixels)*12, 1<<20, int64(pixels), cfg.AggregateRuntimeLockID))
			} else if l.Phase == "color_fit" {
				if len(alignments[p.PairID]) != 1 {
					return false, errors.New("missing fit alignment")
				}
				v := alignments[p.PairID][0]
				sourceRef := p.Before.ref()
				source := before
				if cfg.Options.GeometryEnabled && l.GraphVersion >= 2 {
					if len(warped[p.PairID]) != 1 {
						return false, errors.New("missing geometry image")
					}
					sourceRef = warped[p.PairID][0].Ref
					source, err = readWorking(ctx, c.Objects, c.Bucket, warped[p.PairID][0].Object)
					if err != nil {
						return false, err
					}
				}
				var ignore *styleIgnoreMask
				inputs := append(workingInputs(sourceRef, source), workingInputs(p.After.ref(), after)...)
				inputs = append(inputs, v.Ref)
				if p.IgnoreMask != nil {
					ignore = &styleIgnoreMask{Artifact: p.IgnoreMask.ref(), Canvas: after.Canvas, Transform: after.TransformChain, Selector: "ignore", Confidence: 1, Protected: true}
					inputs = append(inputs, p.IgnoreMask.ref())
				}
				params, _ := canonical.Marshal(struct {
					PairID    uuid.UUID          `json:"pair_id"`
					Before    review.ArtifactRef `json:"before"`
					After     review.ArtifactRef `json:"after"`
					Alignment review.ArtifactRef `json:"alignment"`
					Ignore    *styleIgnoreMask   `json:"ignore"`
					Seed      uint32             `json:"seed"`
					Samples   int                `json:"samples"`
					Local     bool               `json:"local_requested"`
					Geometry  string             `json:"geometry_mode"`
				}{p.PairID, sourceRef, p.After.ref(), v.Ref, ignore, cfg.Options.Seed, 65536, false, "similarity_only"})
				plans = append(plans, makeStylePlan(l, registry, "color_fit", p.Ordinal, &p.PairID, "pair.fit.color", cfg.FitBackend, inputs, []uuid.UUID{v.TaskID}, params, 4<<30, map[string]int64{"cpu": 0, "mps": 1 << 30, "cuda": 1 << 30}[cfg.FitBackend], pixels*20, 8<<20, pixels, cfg.FitRuntimeLockID))
			}
		}
	}
	for _, p := range plans {
		renewed, renewErr := c.Store.renew(ctx, l)
		if renewErr != nil || !renewed {
			if renewErr == nil {
				renewErr = errors.New("style analysis lease lost")
			}
			return false, renewErr
		}
		ok, e := c.Store.register(ctx, l, p.plan)
		if e != nil || !ok {
			if e == nil {
				e = errors.New("style node registration rejected")
			}
			return false, e
		}
		ref, e := c.Publisher.Publish(ctx, artifactjson.Scope{Tenant: l.TenantID, Run: l.RunID, Owner: p.plan.ID, Purpose: "task_params"}, p.plan.Params)
		if e != nil {
			return false, e
		}
		p.task.Params = review.ArtifactRef{ID: ref.ID, SHA256: ref.SHA256}
		p.task.InputDigest, e = repairInputDigest(p.task.Operation, p.task.Version, p.task.Params, p.task.Inputs)
		if e != nil {
			return false, e
		}
		ok, e = c.Store.materialize(ctx, l, p.task)
		if e != nil || !ok {
			if e == nil {
				e = errors.New("style task materialization rejected")
			}
			return false, e
		}
	}
	next := map[string]string{"registration": "color_fit", "features": "body_fit", "body_fit": "geometry", "geometry": "geometry_registration", "geometry_registration": "color_fit", "color_fit": "aggregate", "face_detect": "face_semantics", "face_semantics": "face_skin", "face_skin": "effects_fit", "effects_fit": "aggregate", "aggregate": "complete", "holdout_features": "holdout_registration", "holdout_registration": "validate", "validate": "package", "package": "complete"}[l.Phase]
	if l.Phase == "registration" && cfg.Options.GeometryEnabled && l.GraphVersion >= 2 {
		next = "features"
	}
	if l.Phase == "aggregate" && l.GraphVersion >= 3 && cfg.HoldoutSnapshotID != nil {
		next = "holdout_features"
	}
	if l.Phase == "color_fit" && l.GraphVersion >= 3 {
		next = "face_detect"
	}
	ok, err := c.Store.advance(ctx, l, next)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, errors.New("style analysis lease lost")
	}
	return true, nil
}

func makeStylePlan(l StyleAnalysisLease, r Registry, phase string, ordinal int, pair *uuid.UUID, operation, backend string, inputs []review.ArtifactRef, prereq []uuid.UUID, params []byte, ram, vram, scratch, output, pixels int64, lock string) plannedStyle {
	op := r[operation]
	key := fmt.Sprintf("%s/%02d", operation, ordinal)
	if phase == "geometry_registration" || phase == "holdout_features" || phase == "holdout_registration" {
		key = fmt.Sprintf("%s/%s/%02d", phase, operation, ordinal)
	}
	id := nodeID(l.RunID, key)
	sum := sha256.Sum256(params)
	prof, _ := styleAnalysisProfile(backend, lock, ram, vram, scratch, output, pixels)
	return plannedStyle{styleNodePlan{id, phase, ordinal, pair, key, operation, append([]review.ArtifactRef(nil), inputs...), append([]uuid.UUID(nil), prereq...), hex.EncodeToString(sum[:]), params}, PreparedTask{ID: id, NodeKey: key, Operation: operation, Version: op.Version, ResourceClass: op.ResourceClass, TimeoutSeconds: op.TimeoutSeconds, Inputs: append([]review.ArtifactRef(nil), inputs...), ExecutionProfile: prof}}
}

type plannedStyle struct {
	plan styleNodePlan
	task PreparedTask
}

func (c StyleAnalysisCoordinator) ProcessOne(ctx context.Context) (bool, error) {
	l, err := c.Store.Claim(ctx)
	if err != nil || l == nil {
		return false, err
	}
	done, err := c.build(ctx, *l)
	if err != nil {
		// Object storage and database failures are retryable. The fenced lease
		// expires and the exact immutable plans/documents are replayed.
		return false, err
	}
	return done, nil
}

var _ artifactjson.Repository = StyleAnalysisDocuments{}
