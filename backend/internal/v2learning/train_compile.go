package v2learning

import (
	"bytes"
	"reflect"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// TrainingCaseEvidence is supplied by the immutable artifact resolver for a
// relationally eligible correction. It cannot be synthesized from request JSON.
type TrainingCaseEvidence struct {
	Source   EligibleTrainingExample
	Resolved ResolvedTrainingCase
	Frozen   *CompiledTrainingRecipes
	Defect   *PreparedReviewedDefectHoldout
	Dust     *PreparedReviewedDustHoldout
	Detail   *PreparedPortraitDetailHoldout
}

type TrainingCompilation struct {
	Draft           TrainingDraftPrepared
	Excluded        []ExcludedTrainingCandidate
	FrozenRecipes   map[uuid.UUID]CompiledTrainingRecipes
	ReviewedDefects map[uuid.UUID]PreparedReviewedDefectHoldout
	ReviewedDust    map[uuid.UUID]PreparedReviewedDustHoldout
	PortraitDetail  map[uuid.UUID]PreparedPortraitDetailHoldout
}

// CompileTrainingDraft joins the locked candidate inventory with resolved
// artifacts, freezes the session split and produces the exact draft bytes.
// The caller still publishes and freezes those bytes in the same admission
// protocol before scheduling Fit.
func CompileTrainingDraft(tenantID, actorID, authSessionID,
	projectID, profileID uuid.UUID, key string, reservedBytes int64,
	request TrainCreate, context ManifestContext,
	candidates []TrainingCandidate,
	evidence []TrainingCaseEvidence) (TrainingCompilation, error) {
	return CompileTrainingDraftWithExclusions(tenantID, actorID,
		authSessionID, projectID, profileID, key, reservedBytes,
		request, context, candidates, evidence, nil)
}

// CompileTrainingDraftWithExclusions accepts per-event artifact exclusions
// only when they are explicit and disjoint from evidence. This prevents an
// incomplete resolver from silently shrinking the frozen training corpus.
func CompileTrainingDraftWithExclusions(tenantID, actorID, authSessionID,
	projectID, profileID uuid.UUID, key string, reservedBytes int64,
	request TrainCreate, context ManifestContext,
	candidates []TrainingCandidate, evidence []TrainingCaseEvidence,
	artifactExcluded []ExcludedTrainingCandidate) (TrainingCompilation, error) {
	var empty TrainingCompilation
	if _, err := ValidateTrainCreate(request); err != nil ||
		context.ProfileID != profileID || context.ProjectID != projectID ||
		context.Mode != request.Policy.Mode ||
		context.Calibration != request.Policy.Calibration ||
		len(candidates) > 5000 || len(evidence) > 5000 ||
		len(artifactExcluded) > 5000 {
		return empty, ErrInvalidTrainingDraft
	}
	policySHA, err := canonical.Hash(request.Policy)
	if err != nil || context.PolicySHA != policySHA {
		return empty, ErrInvalidTrainingDraft
	}
	selected := make(map[uuid.UUID]bool, len(request.SessionIDs))
	for _, id := range request.SessionIDs {
		selected[id] = true
	}
	byID := make(map[uuid.UUID]TrainingCandidate, len(candidates))
	for _, item := range candidates {
		if item.EventID == uuid.Nil || byID[item.EventID].EventID != uuid.Nil ||
			!selected[item.SessionID] {
			return empty, ErrInvalidTrainingDraft
		}
		byID[item.EventID] = item
	}
	eligible, excluded := PreselectTrainingCandidates(candidates)
	eligibleID := make(map[uuid.UUID]bool, len(eligible))
	for _, item := range eligible {
		eligibleID[item.EventID] = true
	}
	byEvidence := make(map[uuid.UUID]TrainingCaseEvidence, len(evidence))
	for _, item := range evidence {
		if item.Source.EventID == uuid.Nil || !eligibleID[item.Source.EventID] ||
			byEvidence[item.Source.EventID].Source.EventID != uuid.Nil {
			return empty, ErrInvalidTrainingDraft
		}
		byEvidence[item.Source.EventID] = item
	}
	byExcluded := make(map[uuid.UUID]bool, len(artifactExcluded))
	for _, item := range artifactExcluded {
		if item.EventID == uuid.Nil || !eligibleID[item.EventID] ||
			byExcluded[item.EventID] ||
			item.Reason != "UNSUPPORTED_ARTIFACT" ||
			byEvidence[item.EventID].Source.EventID != uuid.Nil {
			return empty, ErrInvalidTrainingDraft
		}
		byExcluded[item.EventID] = true
	}
	if len(byEvidence)+len(byExcluded) != len(eligible) {
		return empty, ErrInvalidTrainingDraft
	}
	sources := make([]EligibleTrainingExample, 0, len(eligible))
	for _, candidate := range eligible {
		if byExcluded[candidate.EventID] {
			continue
		}
		item, okay := byEvidence[candidate.EventID]
		if !okay || !reflect.DeepEqual(item.Resolved.Candidate, candidate) ||
			item.Source.EventID != candidate.EventID ||
			item.Source.SessionID != candidate.SessionID ||
			item.Source.SourceContentSHA != candidate.SourceContentSHA ||
			item.Source.SourceLineageSHA != candidate.SourceLineageSHA ||
			!item.Source.ApprovedAt.Equal(candidate.ApprovedAt) {
			return empty, ErrInvalidTrainingDraft
		}
		sources = append(sources, item.Source)
	}
	if len(sources)+len(byExcluded) != len(eligible) {
		return empty, ErrInvalidTrainingDraft
	}
	split, err := FreezeTrainingSplit(sources)
	if err != nil {
		return empty, err
	}
	resolved := make(map[uuid.UUID]ResolvedTrainingCase, len(split.Examples))
	frozenRecipes := make(map[uuid.UUID]CompiledTrainingRecipes, len(split.Examples))
	reviewedDefects := make(map[uuid.UUID]PreparedReviewedDefectHoldout)
	reviewedDust := make(map[uuid.UUID]PreparedReviewedDustHoldout)
	portraitDetail := make(map[uuid.UUID]PreparedPortraitDetailHoldout)
	runID, err := v2input.RunID(tenantID, actorID, key)
	if err != nil {
		return empty, ErrInvalidTrainingDraft
	}
	for _, item := range split.Examples {
		evidence := byEvidence[item.Source.EventID]
		entry := evidence.Resolved
		if evidence.Frozen == nil ||
			entry.Render.BaseRecipe.SHA256 != evidence.Frozen.BaseSHA ||
			entry.Render.ApprovedRecipe.SHA256 != evidence.Frozen.ApprovedSHA {
			return empty, ErrInvalidTrainingDraft
		}
		frozenRecipes[item.Source.EventID] = *evidence.Frozen
		baseRef, err := TrainingFrozenRef(runID, item.Source.EventID, "base", entry.Render.BaseRecipe.SHA256)
		if err != nil {
			return empty, ErrInvalidTrainingDraft
		}
		approvedRef, err := TrainingFrozenRef(runID, item.Source.EventID, "approved", entry.Render.ApprovedRecipe.SHA256)
		if err != nil {
			return empty, ErrInvalidTrainingDraft
		}
		entry.Render.BaseRecipe = baseRef
		entry.Render.ApprovedRecipe = approvedRef
		if evidence.Defect != nil {
			if entry.Render.DefectEvidence != nil ||
				evidence.Defect.Document.TenantID != tenantID ||
				evidence.Defect.Document.EventID != item.Source.EventID ||
				evidence.Defect.Document.AssetVersionID != entry.Candidate.AssetVersionID ||
				evidence.Defect.Document.Image != entry.Render.WorkingImage ||
				evidence.Defect.Document.Protection != entry.Render.Protected {
				return empty, ErrInvalidTrainingDraft
			}
			ref, err := ReviewedDefectHoldoutRef(runID,
				item.Source.EventID, evidence.Defect.SHA256)
			if err != nil {
				return empty, err
			}
			if _, err := DecodeFrozenDefectHoldoutEvidence(
				evidence.Defect.Body, ref,
				defectHoldoutExpected(evidence.Defect.Document)); err != nil {
				return empty, err
			}
			entry.Render.DefectEvidence = &ref
			owned := *evidence.Defect
			owned.Body = append([]byte(nil), owned.Body...)
			owned.Document.Selection.Accepted = append([]string(nil),
				owned.Document.Selection.Accepted...)
			owned.Document.Selection.Rejected = append([]string(nil),
				owned.Document.Selection.Rejected...)
			reviewedDefects[item.Source.EventID] = owned
		}
		if evidence.Dust != nil {
			if evidence.Defect != nil || evidence.Detail != nil ||
				entry.Render.DustEvidence != nil ||
				entry.Render.DefectEvidence != nil ||
				evidence.Dust.Document.TenantID != tenantID ||
				evidence.Dust.Document.EventID != item.Source.EventID ||
				evidence.Dust.Document.AssetVersionID != entry.Candidate.AssetVersionID ||
				evidence.Dust.Document.Image != entry.Render.WorkingImage ||
				evidence.Dust.Document.Protection != entry.Render.Protected {
				return empty, ErrInvalidTrainingDraft
			}
			ref, err := ReviewedDustHoldoutRef(runID,
				item.Source.EventID, evidence.Dust.SHA256)
			if err != nil {
				return empty, err
			}
			if _, err := DecodeFrozenDustHoldoutEvidence(evidence.Dust.Body,
				ref, dustHoldoutExpected(evidence.Dust.Document)); err != nil {
				return empty, err
			}
			entry.Render.DustEvidence = &ref
			owned := *evidence.Dust
			owned.Body = append([]byte(nil), owned.Body...)
			owned.Document.Selection.Accepted = append([]string(nil),
				owned.Document.Selection.Accepted...)
			owned.Document.Selection.Rejected = append([]string(nil),
				owned.Document.Selection.Rejected...)
			reviewedDust[item.Source.EventID] = owned
		}
		if evidence.Detail != nil {
			owned := *evidence.Detail
			if evidence.Defect != nil || evidence.Dust != nil ||
				entry.Render.DefectEvidence != nil ||
				entry.Render.DustEvidence != nil ||
				entry.Render.DetailEvidence != nil ||
				owned.Document.TenantID != tenantID ||
				owned.Document.EventID != item.Source.EventID ||
				owned.Document.AssetVersionID != entry.Candidate.AssetVersionID ||
				owned.Document.Source != entry.Render.WorkingImage ||
				owned.Document.Protection != entry.Render.Protected {
				return empty, ErrInvalidTrainingDraft
			}
			ref, err := PortraitDetailHoldoutRef(runID,
				item.Source.EventID, owned.SHA256)
			if err != nil {
				return empty, err
			}
			if _, err := DecodeFrozenPortraitDetailHoldoutEvidence(
				owned.Body, ref, detailHoldoutExpected(owned.Document)); err != nil {
				return empty, err
			}
			entry.Render.DetailEvidence = &ref
			owned.Body = append([]byte(nil), owned.Body...)
			owned.Document.MaskArtifacts = append([]v2input.Ref(nil),
				owned.Document.MaskArtifacts...)
			portraitDetail[item.Source.EventID] = owned
		}
		if entry.Render.DetailEvidence != nil && evidence.Detail == nil {
			return empty, ErrInvalidTrainingDraft
		}
		entry.Render.BaseDocument = v2input.Ref{ArtifactID: entry.Candidate.BaseArtifactID, SHA256: entry.Candidate.BaseSHA}
		entry.Render.ApprovedDocument = v2input.Ref{ArtifactID: entry.Candidate.ApprovedArtifactID, SHA256: entry.Candidate.ApprovedSHA}
		resolved[item.Source.EventID] = entry
	}
	manifest, _, _, err := CompileTrainingManifest(context, split, resolved)
	if err != nil {
		return empty, err
	}
	draft, err := PrepareTrainingDraft(tenantID, actorID, authSessionID,
		projectID, profileID, key, reservedBytes, request, manifest)
	if err != nil {
		return empty, err
	}
	draft, err = AttachTrainingFrozenRecovery(draft, frozenRecipes)
	if err != nil {
		return empty, err
	}
	draft, err = AttachReviewedDefectRecovery(draft, reviewedDefects)
	if err != nil {
		return empty, err
	}
	draft, err = AttachReviewedDustRecovery(draft, reviewedDust)
	if err != nil {
		return empty, err
	}
	draft, err = AttachPortraitDetailRecovery(draft, portraitDetail)
	if err != nil {
		return empty, err
	}
	excluded = append(excluded, artifactExcluded...)
	excluded = append(excluded, split.Excluded...)
	sort.Slice(excluded, func(i, j int) bool {
		return bytes.Compare(excluded[i].EventID[:], excluded[j].EventID[:]) < 0
	})
	return TrainingCompilation{Draft: draft, Excluded: excluded,
		FrozenRecipes: frozenRecipes, ReviewedDefects: reviewedDefects,
		ReviewedDust: reviewedDust, PortraitDetail: portraitDetail}, nil
}
