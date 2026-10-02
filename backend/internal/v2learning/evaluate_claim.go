package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

type LearningEvaluateLease struct {
	ExpansionID uuid.UUID
	Token       uuid.UUID
	Until       time.Time
	TenantID    uuid.UUID
	RunID       uuid.UUID
	WorkflowID  uuid.UUID
	Candidate   v2input.Ref
}

type LearningEvaluateParams struct {
	SchemaVersion   int         `json:"schema_version"`
	Candidate       v2input.Ref `json:"candidate"`
	Manifest        v2input.Ref `json:"manifest"`
	Baseline        v2input.Ref `json:"baseline"`
	Calibration     v2input.Ref `json:"calibration"`
	ModelLockSHA    string      `json:"model_lock_sha"`
	ICCLockSHA      string      `json:"icc_lock_sha"`
	RendererVersion string      `json:"renderer_version"`
}

type SQLLearningEvaluateStore struct{ DB *pgxpool.Pool }

func validLearningEvaluateLease(lease LearningEvaluateLease) bool {
	return lease.ExpansionID != uuid.Nil && lease.Token != uuid.Nil &&
		!lease.Until.IsZero() && lease.TenantID != uuid.Nil &&
		lease.RunID != uuid.Nil && lease.WorkflowID != uuid.Nil &&
		validManifestRef(lease.Candidate)
}

// Claim reads only the canonical candidate selected by the fenced SQL barrier.
func (s SQLLearningEvaluateStore) Claim(ctx context.Context) (*LearningEvaluateLease, error) {
	if s.DB == nil {
		return nil, ErrInvalidTrainingDraft
	}
	var lease LearningEvaluateLease
	err := s.DB.QueryRow(ctx, `SELECT expansion_id,lease_token,lease_until,
	 tenant_id,run_id,workflow_id,candidate_artifact_id,candidate_sha
	 FROM photo.claim_v23_learning_evaluate()`).Scan(&lease.ExpansionID,
		&lease.Token, &lease.Until, &lease.TenantID, &lease.RunID,
		&lease.WorkflowID, &lease.Candidate.ArtifactID,
		&lease.Candidate.SHA256)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !validLearningEvaluateLease(lease) {
		return nil, ErrInvalidTrainingDraft
	}
	return &lease, nil
}

func (s SQLLearningEvaluateStore) Renew(ctx context.Context,
	lease LearningEvaluateLease) (bool, error) {
	if s.DB == nil || !validLearningEvaluateLease(lease) {
		return false, ErrInvalidTrainingDraft
	}
	var okay bool
	err := s.DB.QueryRow(ctx, `SELECT photo.renew_v23_learning_evaluate($1,$2)`,
		lease.ExpansionID, lease.Token).Scan(&okay)
	return okay, err
}

func (s SQLLearningEvaluateStore) Release(ctx context.Context,
	lease LearningEvaluateLease) (bool, error) {
	if s.DB == nil || !validLearningEvaluateLease(lease) {
		return false, ErrInvalidTrainingDraft
	}
	var okay bool
	err := s.DB.QueryRow(ctx, `SELECT photo.release_v23_learning_evaluate($1,$2)`,
		lease.ExpansionID, lease.Token).Scan(&okay)
	return okay, err
}

// CompileEvaluateParams rejects any candidate other than the leased canonical
// fit result and pins the exact frozen manifest, baseline and lock versions.
func CompileEvaluateParams(lease LearningEvaluateLease,
	draft TrainingDraftPrepared) ([]byte, error) {
	if !validLearningEvaluateLease(lease) ||
		lease.TenantID != draft.TenantID || lease.RunID != draft.RunID ||
		lease.WorkflowID != draft.WorkflowID ||
		draft.Manifest.SchemaVersion != 1 ||
		draft.Manifest.RendererVersion != "v2.2" ||
		!validManifestRef(draft.Manifest.Baseline) ||
		!validManifestRef(draft.Manifest.Calibration) ||
		!validSHA(draft.Manifest.ModelLockSHA) ||
		!validSHA(draft.Manifest.ICCLockSHA) ||
		!validSHA(draft.ManifestSHA) ||
		len(draft.Manifest.Examples) < 40 {
		return nil, ErrInvalidTrainingDraft
	}
	manifestSHA, err := canonical.Hash(draft.Manifest)
	if err != nil || manifestSHA != draft.ManifestSHA {
		return nil, ErrInvalidTrainingDraft
	}
	manifestID, err := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	if err != nil {
		return nil, err
	}
	params := LearningEvaluateParams{SchemaVersion: 1,
		Candidate:       lease.Candidate,
		Manifest:        v2input.Ref{ArtifactID: manifestID, SHA256: draft.ManifestSHA},
		Baseline:        draft.Manifest.Baseline,
		Calibration:     draft.Manifest.Calibration,
		ModelLockSHA:    draft.Manifest.ModelLockSHA,
		ICCLockSHA:      draft.Manifest.ICCLockSHA,
		RendererVersion: "v2.2"}
	body, err := canonical.Marshal(params)
	if err != nil || len(body) < 2 || len(body) > 1048576 {
		return nil, ErrInvalidTrainingDraft
	}
	return body, nil
}

func PublishEvaluateParams(ctx context.Context, publisher artifactjson.Publisher,
	lease LearningEvaluateLease, draft TrainingDraftPrepared) (artifactjson.Ref, error) {
	body, err := CompileEvaluateParams(lease, draft)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	return publisher.Publish(ctx, artifactjson.Scope{Tenant: lease.TenantID,
		Run: lease.RunID, Owner: lease.ExpansionID,
		Purpose: "v23_evaluate_params"}, body)
}

type EvaluateStageResult struct {
	TaskID  uuid.UUID
	Params  artifactjson.Ref
	Closure FitClosure
}

// LearningRenderInputs comes from a trusted reader of the frozen documents.
// Every holdout case must supply its nested pixel and protected-mask bytes;
// Extras carry any additional refs exposed by its StyleV2/transform documents.
type LearningRenderInputs struct {
	EventID            uuid.UUID
	WorkingImage       v2input.Ref
	PixelCount         int64
	Pixels             v2input.Ref
	Alpha              *v2input.Ref
	Protected          v2input.Ref
	ProtectedPixels    v2input.Ref
	EffectiveMask      *v2input.Ref
	EffectivePixels    *v2input.Ref
	ConsentSnapshot    *v2input.Ref
	RegionMasks        []LearningRegionPixels
	BaseStyle          v2input.Ref
	BaseRecipeBody     []byte
	ApprovedRecipeBody []byte
	Extras             []v2input.Entry
}

type LearningRegionPixels struct {
	RegionID string
	Mask     v2input.Ref
	Pixels   v2input.Ref
}

type evaluateResourceEstimate struct {
	RAMBytes     int64 `json:"ram_bytes"`
	VRAMBytes    int64 `json:"vram_bytes"`
	ScratchBytes int64 `json:"scratch_bytes"`
	OutputBytes  int64 `json:"output_bytes"`
	PixelCount   int64 `json:"pixel_count"`
}

type evaluateProfile struct {
	Backend          string                   `json:"backend"`
	RendererVersion  string                   `json:"renderer_version"`
	ResourceEstimate evaluateResourceEstimate `json:"resource_estimate"`
}

// StageEvaluateTask admits the canonical fitted candidate only while its
// expansion lease is live. SQL repeats the fence and artifact checks under
// locks, so a stale coordinator cannot stage a different candidate.
func (s TrainingInputStager) StageEvaluateTask(ctx context.Context,
	lease LearningEvaluateLease, draft TrainingDraftPrepared,
	resolved []LearningRenderInputs) (EvaluateStageResult, error) {
	if s.DB == nil || s.Publisher.Repository == nil ||
		s.Publisher.Objects == nil || !validLearningEvaluateLease(lease) {
		return EvaluateStageResult{}, ErrInvalidTrainingDraft
	}
	task, err := v2input.TaskID(lease.WorkflowID, "learning.evaluate", 0)
	if err != nil {
		return EvaluateStageResult{}, err
	}
	params, err := PublishEvaluateParams(ctx, s.Publisher, lease, draft)
	if err != nil {
		return EvaluateStageResult{}, err
	}
	closure, err := CompileEvaluateClosure(lease, draft, params, resolved)
	if err != nil {
		return EvaluateStageResult{}, err
	}
	err = database.InTransaction(ctx, s.DB, func(tx pgx.Tx) error {
		var staged bool
		err := tx.QueryRow(ctx, `SELECT photo.stage_v23_learning_evaluate(
		 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11::jsonb)`,
			lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
			task, params.ID, params.SHA256, lease.Candidate.ArtifactID,
			lease.Candidate.SHA256, closure.InputDigest,
			string(closure.Profile)).Scan(&staged)
		if err != nil {
			return err
		}
		if !staged {
			return ErrTrainingDraftConflict
		}
		return nil
	})
	if err != nil {
		return EvaluateStageResult{}, err
	}
	return EvaluateStageResult{TaskID: task, Params: params, Closure: closure}, nil
}

// CompileEvaluateClosure refuses admission if a holdout image has no
// pinned pixels, protected mask pixels or resolved base style. It also records
// caller-resolved recursive refs in the worker's immutable gateway allowlist.
func CompileEvaluateClosure(lease LearningEvaluateLease,
	draft TrainingDraftPrepared, params artifactjson.Ref,
	resolved []LearningRenderInputs) (FitClosure, error) {
	if _, err := CompileEvaluateParams(lease, draft); err != nil {
		return FitClosure{}, err
	}
	if !validManifestRef(v2input.Ref{ArtifactID: params.ID, SHA256: params.SHA256}) {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	manifestID, err := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	if err != nil {
		return FitClosure{}, err
	}
	paramsRef := v2input.Ref{ArtifactID: params.ID, SHA256: params.SHA256}
	manifestRef := v2input.Ref{ArtifactID: manifestID, SHA256: draft.ManifestSHA}
	entries := []v2input.Entry{
		{Ref: paramsRef, Contract: "Params"},
		{Ref: manifestRef, Contract: "TrainingManifest"},
		{Ref: lease.Candidate, Contract: "CandidateProfile"},
		{Ref: draft.Manifest.Baseline, Contract: "CandidateProfile"},
		{Ref: draft.Manifest.Calibration, Contract: "Calibration"},
	}
	holdout := 0
	var totalPixels, largestPixels, largestRegionPixels int64
	byEvent := make(map[uuid.UUID]LearningRenderInputs, len(resolved))
	for _, item := range resolved {
		if item.EventID == uuid.Nil {
			return FitClosure{}, ErrInvalidTrainingDraft
		}
		if _, duplicate := byEvent[item.EventID]; duplicate {
			return FitClosure{}, ErrInvalidTrainingDraft
		}
		byEvent[item.EventID] = item
	}
	for _, example := range draft.Manifest.Examples {
		if example.Split != "holdout" {
			continue
		}
		holdout++
		c := example.RenderCase
		{
			item, okay := byEvent[example.EventID]
			baseSum := sha256.Sum256(item.BaseRecipeBody)
			approvedSum := sha256.Sum256(item.ApprovedRecipeBody)
			style, recipeErr := ValidateFrozenRecipePair(
				item.BaseRecipeBody, item.ApprovedRecipeBody, c,
				draft.Manifest)
			if !okay || item.WorkingImage != c.WorkingImage ||
				item.Protected != c.Protected ||
				(c.EffectiveMask == nil) != (c.ConsentSnapshot == nil) ||
				(c.EffectiveMask == nil) != (item.EffectiveMask == nil) ||
				(c.ConsentSnapshot == nil) != (item.ConsentSnapshot == nil) ||
				(c.EffectiveMask == nil && item.EffectivePixels != nil) ||
				(c.EffectiveMask != nil &&
					(*c.EffectiveMask != *item.EffectiveMask ||
						*c.ConsentSnapshot != *item.ConsentSnapshot ||
						item.EffectivePixels == nil ||
						!validManifestRef(*item.EffectivePixels))) ||
				len(c.RegionMasks) != len(item.RegionMasks) ||
				recipeErr != nil || style != item.BaseStyle ||
				hex.EncodeToString(baseSum[:]) != c.BaseRecipe.SHA256 ||
				hex.EncodeToString(approvedSum[:]) != c.ApprovedRecipe.SHA256 ||
				item.PixelCount < 1 || item.PixelCount > 200_000_000 ||
				totalPixels > 1_000_000_000_000-item.PixelCount ||
				!validManifestRef(item.Pixels) ||
				!validManifestRef(item.ProtectedPixels) ||
				!validManifestRef(item.BaseStyle) ||
				(item.Alpha != nil && !validManifestRef(*item.Alpha)) {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			totalPixels += item.PixelCount
			if item.PixelCount > largestPixels {
				largestPixels = item.PixelCount
			}
			if regionPixels := item.PixelCount * int64(len(c.RegionMasks)); regionPixels > largestRegionPixels {
				largestRegionPixels = regionPixels
			}
			entries = append(entries,
				v2input.Entry{Ref: item.Pixels, Contract: "NpyArray"},
				v2input.Entry{Ref: item.ProtectedPixels, Contract: "MaskArray"},
				v2input.Entry{Ref: item.BaseStyle, Contract: "StyleV2"})
			if item.Alpha != nil {
				entries = append(entries, v2input.Entry{Ref: *item.Alpha,
					Contract: "NpyArray"})
			}
			entries = append(entries, item.Extras...)
			for index, region := range c.RegionMasks {
				resolved := item.RegionMasks[index]
				if len(region.RegionID) < 1 || len(region.RegionID) > 120 ||
					region.RegionID != resolved.RegionID ||
					region.Mask != resolved.Mask ||
					!validManifestRef(resolved.Pixels) {
					return FitClosure{}, ErrInvalidTrainingDraft
				}
				entries = append(entries, v2input.Entry{
					Ref: resolved.Pixels, Contract: "MaskArray"})
			}
			if item.EffectivePixels != nil {
				entries = append(entries, v2input.Entry{
					Ref: *item.EffectivePixels, Contract: "MaskArray"})
			}
			delete(byEvent, example.EventID)
		}
		entries = append(entries,
			v2input.Entry{Ref: c.WorkingImage, Contract: "WorkingImage"},
			v2input.Entry{Ref: c.BaseRecipe, Contract: "FrozenRecipe"},
			v2input.Entry{Ref: c.ApprovedRecipe, Contract: "FrozenRecipe"},
			v2input.Entry{Ref: c.Protected, Contract: "MaskRef"},
			v2input.Entry{Ref: c.Faces, Contract: "FaceObservations"})
		entries = append(entries, v2input.Entry{Ref: c.DescriptorShard,
			Contract: "DescriptorShard"})
		if c.EffectiveMask != nil {
			entries = append(entries,
				v2input.Entry{Ref: *c.EffectiveMask, Contract: "MaskRef"},
				v2input.Entry{Ref: *c.ConsentSnapshot, Contract: "ConsentSnapshot"})
		}
		for _, region := range c.RegionMasks {
			entries = append(entries, v2input.Entry{
				Ref: region.Mask, Contract: "MaskRef"})
		}
	}
	if holdout < 10 || holdout != len(resolved) || len(byEvent) != 0 {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	unique := make([]v2input.Entry, 0, len(entries))
	for _, e := range entries {
		if !validManifestRef(e.Ref) {
			return FitClosure{}, ErrInvalidTrainingDraft
		}
		if len(unique) > 0 && unique[len(unique)-1].Ref.ArtifactID == e.Ref.ArtifactID {
			if unique[len(unique)-1] != e {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			continue
		}
		unique = append(unique, e)
	}
	refs := make([]v2input.Ref, len(unique))
	for i, e := range unique {
		refs[i] = e.Ref
	}
	digest, err := canonical.Hash(struct {
		Version          string        `json:"version"`
		Operation        string        `json:"operation"`
		OperationVersion string        `json:"operation_version"`
		Renderer         string        `json:"renderer"`
		ManifestSHA      string        `json:"manifest_sha"`
		Candidate        v2input.Ref   `json:"candidate"`
		Params           v2input.Ref   `json:"params"`
		Inputs           []v2input.Ref `json:"inputs"`
	}{"task_input_v2", "learning.evaluate", "1", "v2.2", draft.ManifestSHA, lease.Candidate, paramsRef, refs})
	if err != nil {
		return FitClosure{}, err
	}
	// The largest sequential case keeps pinned source children, three RGB
	// renders, person mask/Stats20 workspace and two float64 metric buffers
	// live together. Reserve their full disk footprint plus transport headroom.
	scratch := int64(96)*largestPixels + 4*largestRegionPixels + 536870912
	if scratch > 16*1024*1024*1024 {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	ram := int64(36)*largestPixels + 1073741824
	if ram < 4294967296 {
		ram = 4294967296
	}
	profileBody, err := canonical.Marshal(evaluateProfile{
		Backend: "cuda", RendererVersion: "v2.2",
		ResourceEstimate: evaluateResourceEstimate{RAMBytes: ram,
			VRAMBytes: 4294967296, ScratchBytes: scratch,
			OutputBytes: 8388608, PixelCount: totalPixels}})
	if err != nil {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	profile := json.RawMessage(profileBody)
	return FitClosure{Open: func(context.Context) (v2input.Cursor, error) { return &fitEntryCursor{entries: unique}, nil },
		Direct:      []v2input.Ref{paramsRef, manifestRef, lease.Candidate, draft.Manifest.Baseline, draft.Manifest.Calibration},
		InputDigest: digest, Profile: profile}, nil
}

// CommitEvaluateClosure registers the already published transport and closes
// the evaluation barrier in the same database transaction. The SQL close is
// fail-closed until learning.evaluate is enabled and its safety worker is
// qualified.
func (a SQLLearningEvaluateStore) CommitEvaluateClosure(ctx context.Context,
	lease LearningEvaluateLease, task v2input.GraphTask) error {
	if a.DB == nil || !validLearningEvaluateLease(lease) {
		return ErrInvalidTrainingDraft
	}
	expected, err := v2input.TaskID(lease.WorkflowID, "learning.evaluate", 0)
	if err != nil || task.ID != expected {
		return ErrInvalidTrainingDraft
	}
	return database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		if err := v2input.RegisterInTransaction(ctx, tx, lease.TenantID,
			lease.RunID, task.ID, task.Built, task.Open, task.Direct); err != nil {
			return err
		}
		var closed bool
		if err := tx.QueryRow(ctx, `SELECT photo.close_v23_learning_evaluate($1,$2,$3,$4,$5)`,
			lease.TenantID, lease.RunID, lease.ExpansionID, lease.Token,
			task.ID).Scan(&closed); err != nil {
			return err
		}
		if !closed {
			return ErrTrainingDraftConflict
		}
		return nil
	})
}

// AdoptVersion validates the exact downloaded Evaluation document before the
// database creates immutable version metadata and consent lineage. The caller
// must fetch the versioned evaluation, baseline, candidate and calibration
// object bytes; all four are checked against the frozen artifact references.
func (a SQLLearningEvaluateStore) AdoptVersion(ctx context.Context,
	draft TrainingDraftPrepared, candidate v2input.Ref,
	evaluation artifactjson.Ref, evaluationBody, baselineBody,
	candidateBody, calibrationBody []byte) (uuid.UUID, error) {
	if a.DB == nil || !validManifestRef(candidate) ||
		!validManifestRef(v2input.Ref{ArtifactID: evaluation.ID,
			SHA256: evaluation.SHA256}) ||
		draft.TenantID == uuid.Nil || draft.RunID == uuid.Nil {
		return uuid.Nil, ErrInvalidTrainingDraft
	}
	evidence, err := VerifyEvaluationEvidence(evaluationBody, evaluation.SHA256,
		draft.Manifest, draft.ManifestSHA, candidate.SHA256)
	if err != nil {
		return uuid.Nil, err
	}
	if err := VerifyEvaluationLosses(evidence, draft.Manifest,
		draft.ManifestSHA, candidate, baselineBody, candidateBody,
		calibrationBody); err != nil {
		return uuid.Nil, err
	}
	manifestBody, err := canonical.Marshal(draft.Manifest)
	if err != nil || len(manifestBody) > 8*1024*1024 {
		return uuid.Nil, ErrInvalidTrainingDraft
	}
	var version *uuid.UUID
	err = database.InTransaction(ctx, a.DB, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT photo.adopt_v23_learning_version(
		 $1,$2,$3::jsonb,$4::jsonb,$5,$6)`, draft.TenantID, draft.RunID,
			string(manifestBody), string(evaluationBody), evaluation.ID,
			evaluation.SHA256).Scan(&version)
	})
	if err != nil {
		return uuid.Nil, err
	}
	if version == nil || *version == uuid.Nil {
		return uuid.Nil, ErrTrainingDraftConflict
	}
	return *version, nil
}
