package v2learning

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type LearningFitParams struct {
	SchemaVersion int            `json:"schema_version"`
	Manifest      v2input.Ref    `json:"manifest"`
	BaseProfile   v2input.Ref    `json:"base_profile"`
	Policy        LearningPolicy `json:"policy"`
}

type FitClosure struct {
	Open        v2input.CursorFactory
	Direct      []v2input.Ref
	InputDigest string
	Profile     json.RawMessage
}

// CompileFitParams binds the worker to the exact frozen manifest and policy.
func CompileFitParams(draft TrainingDraftPrepared, policy LearningPolicy) ([]byte, error) {
	if draft.RunID == uuid.Nil || draft.ProfileID == uuid.Nil ||
		!validSHA(draft.ManifestSHA) || !validManifestRef(draft.Manifest.Baseline) ||
		draft.Manifest.PolicySHA != draft.PolicySHA ||
		draft.Manifest.Calibration != policy.Calibration {
		return nil, ErrInvalidTrainingDraft
	}
	policySHA, err := canonical.Hash(policy)
	if err != nil || policySHA != draft.PolicySHA {
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
	params := LearningFitParams{SchemaVersion: 1,
		Manifest:    v2input.Ref{ArtifactID: manifestID, SHA256: draft.ManifestSHA},
		BaseProfile: draft.Manifest.Baseline, Policy: policy}
	body, err := canonical.Marshal(params)
	if err != nil || len(body) < 2 || len(body) > 1<<20 {
		return nil, ErrInvalidTrainingDraft
	}
	return body, nil
}

func PublishFitParams(ctx context.Context, publisher artifactjson.Publisher,
	draft TrainingDraftPrepared, policy LearningPolicy) (artifactjson.Ref, error) {
	body, err := CompileFitParams(draft, policy)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	task, err := v2input.TaskID(draft.WorkflowID, "learning.fit", 0)
	if err != nil {
		return artifactjson.Ref{}, err
	}
	return publisher.Publish(ctx, artifactjson.Scope{Tenant: draft.TenantID,
		Run: draft.RunID, Owner: task, Purpose: "v23_fit_params"}, body)
}

// CompileFitClosure declares every artifact reachable by learning.fit,
// including all examples, because the adapter validates the whole manifest.
func CompileFitClosure(draft TrainingDraftPrepared, policy LearningPolicy,
	params artifactjson.Ref) (FitClosure, error) {
	_, err := v2input.TaskID(draft.WorkflowID, "learning.fit", 0)
	if err != nil || !validManifestRef(v2input.Ref{ArtifactID: params.ID,
		SHA256: params.SHA256}) || len(draft.Manifest.Examples) < 40 {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	body, err := CompileFitParams(draft, policy)
	if err != nil {
		return FitClosure{}, err
	}
	paramsDigest := sha256.Sum256(body)
	if params.SHA256 != hex.EncodeToString(paramsDigest[:]) {
		return FitClosure{}, ErrInvalidTrainingDraft
	}
	manifestID, _ := TrainingDocumentID(draft.RunID, trainingManifestPurpose)
	manifestRef := v2input.Ref{ArtifactID: manifestID, SHA256: draft.ManifestSHA}
	paramsRef := v2input.Ref{ArtifactID: params.ID, SHA256: params.SHA256}
	entries := []v2input.Entry{
		{Ref: paramsRef, Contract: "Params"},
		{Ref: manifestRef, Contract: "TrainingManifest"},
		{Ref: draft.Manifest.Baseline, Contract: "CandidateProfile"},
		{Ref: draft.Manifest.Calibration, Contract: "Calibration"},
	}
	var detailRecovery map[uuid.UUID]PreparedPortraitDetailHoldout
	var seriesRecovery map[uuid.UUID]PreparedSeriesHoldout
	var seriesStats map[uuid.UUID][]v2input.Ref
	var savedRecovery trainingDraftRecovery
	for _, example := range draft.Manifest.Examples {
		if example.RenderCase.DetailEvidence != nil || example.RenderCase.SeriesEvidence != nil {
			if json.Unmarshal(draft.RecoveryBody, &savedRecovery) != nil {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			break
		}
	}
	for _, example := range draft.Manifest.Examples {
		if example.RenderCase.DetailEvidence != nil {
			detailRecovery, err = portraitDetailFromRecovery(draft,
				[]byte(savedRecovery.DetailJCS))
			if err != nil {
				return FitClosure{}, err
			}
			break
		}
	}
	for _, example := range draft.Manifest.Examples {
		if example.RenderCase.SeriesEvidence != nil {
			seriesRecovery, err = seriesHoldoutFromRecovery(draft,
				[]byte(savedRecovery.SeriesJCS))
			if err != nil {
				return FitClosure{}, err
			}
			seriesStats, err = seriesStatsFromRecovery(draft,
				[]byte(savedRecovery.SeriesStatsJCS))
			if err != nil {
				return FitClosure{}, err
			}
			break
		}
	}
	for _, example := range draft.Manifest.Examples {
		c := example.RenderCase
		if c.DefectEvidence != nil {
			expected, err := ReviewedDefectHoldoutRef(draft.RunID,
				example.EventID, c.DefectEvidence.SHA256)
			if err != nil || expected != *c.DefectEvidence {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries, v2input.Entry{Ref: expected,
				Contract: "FrozenDefectHoldoutEvidence"})
		}
		if c.DustEvidence != nil {
			expected, err := ReviewedDustHoldoutRef(draft.RunID,
				example.EventID, c.DustEvidence.SHA256)
			if err != nil || expected != *c.DustEvidence || c.DefectEvidence != nil {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries, v2input.Entry{Ref: expected,
				Contract: "FrozenDustHoldoutEvidence"})
		}
		if c.DetailEvidence != nil {
			origin, found := detailRecovery[example.EventID]
			if !found || origin.Document.Source != c.WorkingImage ||
				origin.Document.Protection != c.Protected ||
				origin.Document.AssetVersionID != c.Binding.AssetVersionID {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			expected, err := PortraitDetailHoldoutRef(draft.RunID,
				example.EventID, c.DetailEvidence.SHA256)
			if err != nil || expected != *c.DetailEvidence ||
				c.DefectEvidence != nil || c.DustEvidence != nil {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries, v2input.Entry{Ref: expected,
				Contract: "FrozenPortraitDetailHoldoutEvidence"})
			entries = append(entries,
				v2input.Entry{Ref: origin.Document.Image, Contract: "WorkingImage"},
				v2input.Entry{Ref: origin.Document.Params, Contract: "PortraitParams"},
				v2input.Entry{Ref: origin.Document.Resolved, Contract: "ResolvedStyle"})
			for _, mask := range origin.Document.MaskArtifacts {
				entries = append(entries, v2input.Entry{Ref: mask,
					Contract: "NpyArray"})
			}
		}
		if c.SeriesEvidence != nil {
			origin, found := seriesRecovery[example.EventID]
			if !found || origin.Document.TenantID != draft.TenantID ||
				origin.Document.EventID != example.EventID ||
				origin.Document.AssetVersionID != c.Binding.AssetVersionID ||
				origin.Document.SourceImage != c.WorkingImage ||
				c.DefectEvidence != nil || c.DustEvidence != nil || c.DetailEvidence != nil {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			expected, err := SeriesHoldoutRef(draft.RunID,
				example.EventID, origin.SHA256)
			if err != nil || expected != *c.SeriesEvidence {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries,
				v2input.Entry{Ref: expected, Contract: "FrozenSeriesHoldoutEvidence"},
				v2input.Entry{Ref: origin.Document.StageImage, Contract: "WorkingImage"},
				v2input.Entry{Ref: origin.Document.Params, Contract: "SeriesParams"},
				v2input.Entry{Ref: origin.Document.Series, Contract: "SeriesCorrections"})
			for _, ref := range seriesStats[example.EventID] {
				entries = append(entries, v2input.Entry{Ref: ref,
					Contract: "SeriesStats"})
			}
		}
		entries = append(entries,
			v2input.Entry{Ref: c.WorkingImage, Contract: "WorkingImage"},
			v2input.Entry{Ref: c.BaseRecipe, Contract: "FrozenRecipe"},
			v2input.Entry{Ref: c.ApprovedRecipe, Contract: "FrozenRecipe"},
			v2input.Entry{Ref: c.Protected, Contract: "MaskRef"},
			v2input.Entry{Ref: c.Faces, Contract: "FaceObservations"})
		entries = append(entries, v2input.Entry{Ref: c.DescriptorShard,
			Contract: "DescriptorShard"})
		if (c.EffectiveMask == nil) != (c.ConsentSnapshot == nil) {
			return FitClosure{}, ErrInvalidTrainingDraft
		}
		if c.EffectiveMask != nil {
			entries = append(entries,
				v2input.Entry{Ref: *c.EffectiveMask, Contract: "MaskRef"},
				v2input.Entry{Ref: *c.ConsentSnapshot, Contract: "ConsentSnapshot"})
		}
		if c.BackgroundAsset != nil {
			if !validTrainingBackgroundPin(c.BackgroundAsset, draft.Manifest.ICCLockSHA, v2input.Ref{}) {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries, v2input.Entry{Ref: c.BackgroundAsset.Style, Contract: "StyleV2"}, v2input.Entry{Ref: c.BackgroundAsset.Source, Contract: "StyleBackgroundAsset"})
		}
		for _, region := range c.RegionMasks {
			if len(region.RegionID) < 1 || len(region.RegionID) > 120 ||
				!validManifestRef(region.Mask) {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			entries = append(entries, v2input.Entry{
				Ref: region.Mask, Contract: "MaskRef"})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		return bytes.Compare(entries[i].Ref.ArtifactID[:], entries[j].Ref.ArtifactID[:]) < 0
	})
	unique := make([]v2input.Entry, 0, len(entries))
	for _, entry := range entries {
		if !validManifestRef(entry.Ref) {
			return FitClosure{}, ErrInvalidTrainingDraft
		}
		if len(unique) > 0 && unique[len(unique)-1].Ref.ArtifactID == entry.Ref.ArtifactID {
			if unique[len(unique)-1] != entry {
				return FitClosure{}, ErrInvalidTrainingDraft
			}
			continue
		}
		unique = append(unique, entry)
	}
	refs := make([]v2input.Ref, len(unique))
	for i, entry := range unique {
		refs[i] = entry.Ref
	}
	digest, err := canonical.Hash(struct {
		Version          string        `json:"version"`
		Operation        string        `json:"operation"`
		OperationVersion string        `json:"operation_version"`
		Renderer         string        `json:"renderer"`
		ManifestSHA      string        `json:"manifest_sha"`
		PolicySHA        string        `json:"policy_sha"`
		Params           v2input.Ref   `json:"params"`
		Inputs           []v2input.Ref `json:"inputs"`
	}{"task_input_v2", "learning.fit", "1", "v2.2",
		draft.ManifestSHA, draft.PolicySHA, paramsRef, refs})
	if err != nil {
		return FitClosure{}, err
	}
	profile := json.RawMessage(`{"backend":"cpu","renderer_version":"v2.2","resource_estimate":{"ram_bytes":2147483648,"vram_bytes":0,"scratch_bytes":536870912,"output_bytes":8388608,"pixel_count":0}}`)
	return FitClosure{Open: func(ctx context.Context) (v2input.Cursor, error) {
		return openFitEntryCursor(ctx, unique)
	}, Direct: []v2input.Ref{paramsRef, manifestRef,
		draft.Manifest.Baseline, draft.Manifest.Calibration},
		InputDigest: digest, Profile: profile}, nil
}

type fitEntryCursor struct {
	entries []v2input.Entry
	index   int
}

func openFitEntryCursor(ctx context.Context, entries []v2input.Entry) (v2input.Cursor, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &fitEntryCursor{entries: entries}, nil
}

func (c *fitEntryCursor) Next(ctx context.Context) (v2input.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2input.Entry{}, false, err
	}
	if c.index == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	v := c.entries[c.index]
	c.index++
	return v, true, nil
}
