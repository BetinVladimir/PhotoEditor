package v2learning

import (
	"bytes"
	"math"
	"sort"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

// validateDraftManifest checks the immutable envelope again at the public
// draft boundary. Relational grants and artifact bytes are checked by freeze.
func validateDraftManifest(manifest TrainingManifest) bool {
	if manifest.SchemaVersion != 1 || manifest.ProfileID == uuid.Nil ||
		(manifest.Mode != "photographer" && manifest.Mode != "person") ||
		(manifest.Mode == "person") != (manifest.PersonID != nil) ||
		(manifest.PersonID != nil && *manifest.PersonID == uuid.Nil) ||
		manifest.RendererVersion != "v2.2" ||
		!validSHA(manifest.PolicySHA) || !validSHA(manifest.ICCLockSHA) ||
		!validSHA(manifest.ModelLockSHA) ||
		!validManifestRef(manifest.Baseline) ||
		!validManifestRef(manifest.Calibration) ||
		len(manifest.Examples) < 40 || len(manifest.Examples) > 5000 ||
		len(manifest.Consents) < 1 || len(manifest.Consents) > 5000 {
		return false
	}
	consents := make(map[uuid.UUID]bool, len(manifest.Consents))
	for _, item := range manifest.Consents {
		epoch, err := strconv.ParseInt(item.Epoch, 10, 64)
		if item.ConsentID == uuid.Nil || consents[item.ConsentID] ||
			err != nil || epoch < 1 || strconv.FormatInt(epoch, 10) != item.Epoch {
			return false
		}
		consents[item.ConsentID] = true
	}
	sessions := make(map[uuid.UUID]bool)
	events := make(map[uuid.UUID]bool)
	lineages := make(map[string]bool)
	for _, item := range manifest.Examples {
		c := item.RenderCase
		if c.DefectEvidence != nil &&
			(!validManifestRef(*c.DefectEvidence) ||
				c.DefectEvidence.ArtifactID == c.WorkingImage.ArtifactID ||
				c.DefectEvidence.ArtifactID == c.Protected.ArtifactID ||
				c.DefectEvidence.ArtifactID == c.BaseRecipe.ArtifactID ||
				c.DefectEvidence.ArtifactID == c.ApprovedRecipe.ArtifactID) {
			return false
		}
		if c.DustEvidence != nil &&
			(c.DefectEvidence != nil || c.DetailEvidence != nil || !validManifestRef(*c.DustEvidence) ||
				c.DustEvidence.ArtifactID == c.WorkingImage.ArtifactID ||
				c.DustEvidence.ArtifactID == c.Protected.ArtifactID ||
				c.DustEvidence.ArtifactID == c.BaseRecipe.ArtifactID ||
				c.DustEvidence.ArtifactID == c.ApprovedRecipe.ArtifactID) {
			return false
		}
		if c.DetailEvidence != nil &&
			(c.DefectEvidence != nil || c.DustEvidence != nil ||
				!validManifestRef(*c.DetailEvidence) ||
				c.DetailEvidence.ArtifactID == c.WorkingImage.ArtifactID ||
				c.DetailEvidence.ArtifactID == c.Protected.ArtifactID ||
				c.DetailEvidence.ArtifactID == c.BaseRecipe.ArtifactID ||
				c.DetailEvidence.ArtifactID == c.ApprovedRecipe.ArtifactID) {
			return false
		}
		if item.EventID == uuid.Nil || events[item.EventID] ||
			item.SessionID == uuid.Nil || item.SubjectGroupID == uuid.Nil ||
			(item.Split != "train" && item.Split != "holdout") ||
			!validSHA(c.SourceLineageSHA) || lineages[c.SourceLineageSHA] ||
			c.ApprovalID == uuid.Nil || c.Binding.AssetVersionID == uuid.Nil ||
			c.Binding.Source != c.WorkingImage ||
			!validSHA(c.Binding.CanvasHash) ||
			!validManifestRef(c.WorkingImage) ||
			!validManifestRef(c.BaseRecipe) ||
			!validManifestRef(c.ApprovedRecipe) ||
			!validManifestRef(c.DescriptorShard) ||
			c.DescriptorAnalysisID == uuid.Nil ||
			c.DescriptorOrdinal < 0 || c.DescriptorOrdinal > 4999 ||
			!validManifestRef(c.Protected) || !validManifestRef(c.Faces) ||
			(c.EffectiveMask == nil) != (c.ConsentSnapshot == nil) ||
			(c.EffectiveMask != nil && (!validManifestRef(*c.EffectiveMask) ||
				!validManifestRef(*c.ConsentSnapshot))) ||
			len(c.RegionMasks) > 128 ||
			(manifest.PersonID != nil && item.SubjectGroupID != *manifest.PersonID) ||
			len(item.Features) != 12 {
			return false
		}
		if !validTrainingBackgroundPin(c.BackgroundAsset, manifest.ICCLockSHA, v2input.Ref{}) {
			return false
		}
		for _, value := range item.Features {
			if math.IsNaN(value) || math.IsInf(value, 0) || math.Abs(value) > 100 {
				return false
			}
		}
		target := [6]float64{item.Target.EV, item.Target.LogR,
			item.Target.LogG, item.Target.LogB,
			item.Target.SaturationDelta, item.Target.RetouchDelta}
		for index, value := range target {
			if math.IsNaN(value) || math.IsInf(value, 0) ||
				math.Abs(value) > targetLimits[index] {
				return false
			}
		}
		regions := make(map[string]bool, len(c.RegionMasks))
		for _, region := range c.RegionMasks {
			if region.RegionID == "" || len(region.RegionID) > 120 ||
				regions[region.RegionID] || !validManifestRef(region.Mask) {
				return false
			}
			regions[region.RegionID] = true
		}
		events[item.EventID] = true
		lineages[c.SourceLineageSHA] = true
		sessions[item.SessionID] = true
	}
	if len(sessions) < 6 {
		return false
	}
	ordered := make([]uuid.UUID, 0, len(sessions))
	for id := range sessions {
		ordered = append(ordered, id)
	}
	sort.Slice(ordered, func(i, j int) bool {
		left, right := sessionOrder(ordered[i]), sessionOrder(ordered[j])
		if order := bytes.Compare(left[:], right[:]); order != 0 {
			return order < 0
		}
		return bytes.Compare(ordered[i][:], ordered[j][:]) < 0
	})
	holdoutSessions := int(math.Ceil(.2 * float64(len(ordered))))
	if holdoutSessions < 2 {
		holdoutSessions = 2
	}
	if len(ordered)-holdoutSessions < 4 {
		return false
	}
	selected := make(map[uuid.UUID]bool, holdoutSessions)
	for _, id := range ordered[:holdoutSessions] {
		selected[id] = true
	}
	trainCount, holdoutCount := 0, 0
	for _, item := range manifest.Examples {
		if (item.Split == "holdout") != selected[item.SessionID] {
			return false
		}
		if selected[item.SessionID] {
			holdoutCount++
		} else {
			trainCount++
		}
	}
	return trainCount >= 20 && holdoutCount >= 10
}
