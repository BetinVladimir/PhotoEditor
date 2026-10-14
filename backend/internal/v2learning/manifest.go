package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

var ErrInvalidTrainingManifest = errors.New("INVALID_TRAINING_MANIFEST")

type TrainingConsent struct {
	ConsentID uuid.UUID `json:"consent_id"`
	Epoch     string    `json:"epoch"`
}

type TrainingRegionMask struct {
	RegionID string      `json:"region_id"`
	Mask     v2input.Ref `json:"mask"`
}

type TrainingRenderCase struct {
	BackgroundAsset      *v2draft.FrozenBackgroundAsset `json:"background_asset,omitempty"`
	DefectEvidence       *v2input.Ref                   `json:"defect_evidence,omitempty"`
	DustEvidence         *v2input.Ref                   `json:"dust_evidence,omitempty"`
	DetailEvidence       *v2input.Ref                   `json:"detail_evidence,omitempty"`
	SeriesEvidence       *v2input.Ref                   `json:"series_evidence,omitempty"`
	Binding              v2input.SessionVersionBinding  `json:"binding"`
	WorkingImage         v2input.Ref                    `json:"working_image"`
	BaseRecipe           v2input.Ref                    `json:"base_recipe"`
	ApprovedRecipe       v2input.Ref                    `json:"approved_recipe"`
	BaseDocument         v2input.Ref                    `json:"base_document"`
	ApprovedDocument     v2input.Ref                    `json:"approved_document"`
	DescriptorShard      v2input.Ref                    `json:"descriptor_shard"`
	DescriptorAnalysisID uuid.UUID                      `json:"descriptor_analysis_id"`
	DescriptorOrdinal    int                            `json:"descriptor_ordinal"`
	Protected            v2input.Ref                    `json:"protected"`
	Faces                v2input.Ref                    `json:"faces"`
	EffectiveMask        *v2input.Ref                   `json:"effective_mask,omitempty"`
	ConsentSnapshot      *v2input.Ref                   `json:"consent_snapshot,omitempty"`
	RegionMasks          []TrainingRegionMask           `json:"region_masks,omitempty"`
	ApprovalID           uuid.UUID                      `json:"approval_id"`
	SourceLineageSHA     string                         `json:"source_lineage_sha"`
}

type ManifestExample struct {
	EventID        uuid.UUID                `json:"event_id"`
	SessionID      uuid.UUID                `json:"session_id"`
	SubjectGroupID uuid.UUID                `json:"subject_group_id"`
	Split          string                   `json:"split"`
	Features       [12]float64              `json:"features"`
	Target         v2draft.CorrectionVector `json:"target"`
	RenderCase     TrainingRenderCase       `json:"render_case"`
}

type TrainingManifest struct {
	SchemaVersion   int               `json:"schema_version"`
	ProfileID       uuid.UUID         `json:"profile_id"`
	Mode            string            `json:"mode"`
	PersonID        *uuid.UUID        `json:"person_id"`
	Consents        []TrainingConsent `json:"consents"`
	Examples        []ManifestExample `json:"examples"`
	PolicySHA       string            `json:"policy_sha"`
	Baseline        v2input.Ref       `json:"baseline"`
	RendererVersion string            `json:"renderer_version"`
	ICCLockSHA      string            `json:"icc_lock_sha"`
	ModelLockSHA    string            `json:"model_lock_sha"`
	Calibration     v2input.Ref       `json:"calibration"`
}

// ResolvedTrainingCase is supplied only after reading and validating the
// immutable recipe/image/mask/face artifacts. The compiler checks their
// relational identities again against the frozen candidate inventory.
type ResolvedTrainingCase struct {
	Candidate TrainingCandidate
	Render    TrainingRenderCase
}

type ManifestContext struct {
	ProfileID, ProjectID uuid.UUID
	Mode                 string
	PersonID             *uuid.UUID
	PolicySHA            string
	Baseline             v2input.Ref
	ICCLockSHA           string
	ModelLockSHA         string
	Calibration          v2input.Ref
}

func validManifestRef(ref v2input.Ref) bool {
	return ref.ArtifactID != uuid.Nil && validSHA(ref.SHA256)
}

func vectorFromTarget(values [6]float64) v2draft.CorrectionVector {
	return v2draft.CorrectionVector{EV: values[0], LogR: values[1],
		LogG: values[2], LogB: values[3], SaturationDelta: values[4],
		RetouchDelta: values[5]}
}

func sortTrainingConsents(consents []TrainingConsent) {
	sort.Slice(consents, func(i, j int) bool {
		return bytes.Compare(consents[i].ConsentID[:],
			consents[j].ConsentID[:]) < 0
	})
}

func validResolvedTrainingCase(source EligibleTrainingExample,
	item ResolvedTrainingCase) bool {
	candidate, render := item.Candidate, item.Render
	if render.DefectEvidence != nil &&
		(!validManifestRef(*render.DefectEvidence) ||
			render.DefectEvidence.ArtifactID == render.WorkingImage.ArtifactID ||
			render.DefectEvidence.ArtifactID == render.Protected.ArtifactID) {
		return false
	}
	if render.DustEvidence != nil &&
		(render.DefectEvidence != nil || render.DetailEvidence != nil || !validManifestRef(*render.DustEvidence) ||
			render.DustEvidence.ArtifactID == render.WorkingImage.ArtifactID ||
			render.DustEvidence.ArtifactID == render.Protected.ArtifactID) {
		return false
	}
	if render.DetailEvidence != nil &&
		(render.DefectEvidence != nil || render.DustEvidence != nil || render.SeriesEvidence != nil ||
			!validManifestRef(*render.DetailEvidence) ||
			render.DetailEvidence.ArtifactID == render.WorkingImage.ArtifactID ||
			render.DetailEvidence.ArtifactID == render.Protected.ArtifactID) {
		return false
	}
	if render.SeriesEvidence != nil &&
		(render.DefectEvidence != nil || render.DustEvidence != nil ||
			render.DetailEvidence != nil || !validManifestRef(*render.SeriesEvidence) ||
			render.SeriesEvidence.ArtifactID == render.WorkingImage.ArtifactID ||
			render.SeriesEvidence.ArtifactID == render.Protected.ArtifactID) {
		return false
	}
	if candidate.EventID != source.EventID ||
		candidate.SessionID != source.SessionID ||
		candidate.BaseDocumentID == uuid.Nil ||
		candidate.ApprovedDocumentID == uuid.Nil ||
		candidate.BaseDocumentID == candidate.ApprovedDocumentID ||
		candidate.SourceContentSHA != source.SourceContentSHA ||
		candidate.SourceLineageSHA != source.SourceLineageSHA ||
		!candidate.ApprovedAt.Equal(source.ApprovedAt) ||
		!candidate.ConsentLive || !candidate.SourceReady ||
		!candidate.BaseReady || !candidate.ApprovedReady ||
		candidate.ApprovalID == nil || *candidate.ApprovalID == uuid.Nil ||
		render.ApprovalID != *candidate.ApprovalID ||
		render.SourceLineageSHA != source.SourceLineageSHA ||
		render.Binding.AssetVersionID != candidate.AssetVersionID ||
		render.Binding.Source != render.WorkingImage ||
		!validSHA(render.Binding.CanvasHash) ||
		!validManifestRef(render.WorkingImage) ||
		!validManifestRef(render.Protected) || !validManifestRef(render.Faces) ||
		(render.EffectiveMask == nil) != (render.ConsentSnapshot == nil) ||
		(render.EffectiveMask != nil && (!validManifestRef(*render.EffectiveMask) ||
			!validManifestRef(*render.ConsentSnapshot))) ||
		!validManifestRef(render.BaseRecipe) ||
		!validManifestRef(render.ApprovedRecipe) ||
		!validManifestRef(render.DescriptorShard) ||
		render.DescriptorAnalysisID == uuid.Nil ||
		render.DescriptorOrdinal < 0 || render.DescriptorOrdinal > 4999 ||
		render.BaseDocument != (v2input.Ref{ArtifactID: candidate.BaseArtifactID, SHA256: candidate.BaseSHA}) ||
		render.ApprovedDocument != (v2input.Ref{ArtifactID: candidate.ApprovedArtifactID, SHA256: candidate.ApprovedSHA}) ||
		render.BaseRecipe.ArtifactID == render.ApprovedRecipe.ArtifactID ||
		candidate.ConsentID == uuid.Nil || candidate.ConsentEpoch < 1 ||
		len(candidate.ChangedPaths) < 1 {
		return false
	}
	if !validTrainingBackgroundPin(render.BackgroundAsset, "", v2input.Ref{}) {
		return false
	}
	if len(render.RegionMasks) > 128 {
		return false
	}
	regions := make(map[string]bool, len(render.RegionMasks))
	for _, binding := range render.RegionMasks {
		if len(binding.RegionID) < 1 || len(binding.RegionID) > 120 ||
			regions[binding.RegionID] || !validManifestRef(binding.Mask) {
			return false
		}
		regions[binding.RegionID] = true
	}
	for _, path := range candidate.ChangedPaths {
		switch path {
		case "/ev", "/log_r", "/log_g", "/log_b",
			"/saturation_delta", "/retouch_delta":
		default:
			return false
		}
	}
	return true
}

// CompileTrainingManifest serializes the exact, split examples accepted by
// the backend. Its bytes and SHA are passed to the artifact publisher; no
// worker reads live correction rows or chooses a different split.
func CompileTrainingManifest(context ManifestContext, split TrainingSplit,
	cases map[uuid.UUID]ResolvedTrainingCase) (TrainingManifest, []byte, string, error) {
	var empty TrainingManifest
	if context.ProfileID == uuid.Nil || context.ProjectID == uuid.Nil ||
		(context.Mode != "photographer" && context.Mode != "person") ||
		(context.Mode == "person") != (context.PersonID != nil) ||
		(context.PersonID != nil && *context.PersonID == uuid.Nil) ||
		!validSHA(context.PolicySHA) || !validSHA(context.ICCLockSHA) ||
		!validSHA(context.ModelLockSHA) || !validManifestRef(context.Baseline) ||
		!validManifestRef(context.Calibration) || len(split.Examples) < 40 ||
		len(split.Examples) > 5000 || len(split.TrainSessions) < 4 ||
		len(split.HoldoutSessions) < 2 || len(cases) != len(split.Examples) {
		return empty, nil, "", ErrInvalidTrainingManifest
	}
	selectedSources := make([]EligibleTrainingExample, len(split.Examples))
	for i, frozen := range split.Examples {
		selectedSources[i] = frozen.Source
	}
	canonicalSplit, err := FreezeTrainingSplit(selectedSources)
	if err != nil || !reflect.DeepEqual(canonicalSplit.Examples, split.Examples) ||
		!reflect.DeepEqual(canonicalSplit.TrainSessions, split.TrainSessions) ||
		!reflect.DeepEqual(canonicalSplit.HoldoutSessions, split.HoldoutSessions) {
		return empty, nil, "", ErrInvalidTrainingManifest
	}
	out := TrainingManifest{SchemaVersion: 1, ProfileID: context.ProfileID,
		Mode: context.Mode, PersonID: context.PersonID,
		Consents:  make([]TrainingConsent, 0),
		Examples:  make([]ManifestExample, 0, len(split.Examples)),
		PolicySHA: context.PolicySHA, Baseline: context.Baseline,
		RendererVersion: "v2.2", ICCLockSHA: context.ICCLockSHA,
		ModelLockSHA: context.ModelLockSHA, Calibration: context.Calibration}
	seenConsents := make(map[uuid.UUID]int64)
	seenEvents := make(map[uuid.UUID]struct{}, len(split.Examples))
	trainCount, holdoutCount := 0, 0
	trainSessions := make(map[uuid.UUID]struct{}, len(split.TrainSessions))
	holdoutSessions := make(map[uuid.UUID]struct{}, len(split.HoldoutSessions))
	for _, id := range split.TrainSessions {
		if id == uuid.Nil {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		trainSessions[id] = struct{}{}
	}
	for _, id := range split.HoldoutSessions {
		if id == uuid.Nil {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		if _, overlaps := trainSessions[id]; overlaps {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		holdoutSessions[id] = struct{}{}
	}
	for _, frozen := range split.Examples {
		source := frozen.Source
		item, found := cases[source.EventID]
		if !found || !validTrainingExample(source) ||
			!validResolvedTrainingCase(source, item) {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		if _, duplicate := seenEvents[source.EventID]; duplicate {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		seenEvents[source.EventID] = struct{}{}
		if frozen.Split == "train" {
			if _, okay := trainSessions[source.SessionID]; !okay {
				return empty, nil, "", ErrInvalidTrainingManifest
			}
			trainCount++
		} else if frozen.Split == "holdout" {
			if _, okay := holdoutSessions[source.SessionID]; !okay {
				return empty, nil, "", ErrInvalidTrainingManifest
			}
			holdoutCount++
		} else {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		previousEpoch, seen := seenConsents[item.Candidate.ConsentID]
		if seen && previousEpoch != item.Candidate.ConsentEpoch {
			return empty, nil, "", ErrInvalidTrainingManifest
		}
		seenConsents[item.Candidate.ConsentID] = item.Candidate.ConsentEpoch
		subject := context.ProjectID
		if context.PersonID != nil {
			subject = *context.PersonID
		}
		out.Examples = append(out.Examples, ManifestExample{
			EventID: source.EventID, SessionID: source.SessionID,
			SubjectGroupID: subject, Split: frozen.Split,
			Features: source.Features, Target: vectorFromTarget(source.Target),
			RenderCase: item.Render})
	}
	if trainCount < 20 || holdoutCount < 10 || len(seenConsents) == 0 {
		return empty, nil, "", ErrInvalidTrainingManifest
	}
	for consentID, epoch := range seenConsents {
		out.Consents = append(out.Consents, TrainingConsent{
			ConsentID: consentID, Epoch: strconv.FormatInt(epoch, 10)})
	}
	sortTrainingConsents(out.Consents)
	body, err := canonical.Marshal(out)
	if err != nil || len(body) < 2 || len(body) > 8<<20 {
		return empty, nil, "", ErrInvalidTrainingManifest
	}
	digest := sha256.Sum256(body)
	return out, body, hex.EncodeToString(digest[:]), nil
}
