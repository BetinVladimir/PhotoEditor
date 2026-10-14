package v2sessions

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"sort"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
)

// ApplyDecision is the immutable client decision for a completed scene plan.
// The approved group set is final; every omitted group is rejected.
type ApplyDecision struct {
	PlanSHA          string      `json:"plan_sha"`
	ApprovedGroups   []uuid.UUID `json:"approved_groups"`
	Recipe           ArtifactRef `json:"recipe"`
	ExpectedRevision string      `json:"expected_revision"`
}

type scenePlan struct {
	SchemaVersion  int          `json:"schema_version"`
	SnapshotID     uuid.UUID    `json:"snapshot_id"`
	SnapshotDigest string       `json:"snapshot_digest"`
	PolicySHA      string       `json:"policy_sha"`
	Groups         []sceneGroup `json:"groups"`
}

type sceneGroup struct {
	ID               uuid.UUID   `json:"id"`
	Ordinals         []int       `json:"ordinals"`
	ReferenceOrdinal int         `json:"reference_ordinal"`
	Confidence       float64     `json:"confidence"`
	Corrections      ArtifactRef `json:"corrections"`
}

// selectedSceneItem is the exact source ordinal to render and the correction
// document chosen for it. A rejected group contributes no child Apply item.
type selectedSceneItem struct {
	Ordinal     int
	GroupID     uuid.UUID
	Corrections ArtifactRef
}

// projectApplySelection runs only after validateApplyDecision has checked the
// full snapshot partition. Sorting by snapshot ordinal makes the child input
// independent of the client's approved_groups order.
func projectApplySelection(plan scenePlan, approved []uuid.UUID) ([]selectedSceneItem, error) {
	invalid := func() ([]selectedSceneItem, error) {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	if len(approved) == 0 || len(approved) > 5000 {
		return invalid()
	}
	selected := make(map[uuid.UUID]bool, len(approved))
	for _, id := range approved {
		if id == uuid.Nil || selected[id] {
			return invalid()
		}
		selected[id] = true
	}
	items := make([]selectedSceneItem, 0)
	for _, group := range plan.Groups {
		if !selected[group.ID] {
			continue
		}
		delete(selected, group.ID)
		for _, ordinal := range group.Ordinals {
			items = append(items, selectedSceneItem{ordinal, group.ID, group.Corrections})
		}
	}
	if len(selected) != 0 || len(items) == 0 {
		return invalid()
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Ordinal < items[j].Ordinal })
	return items, nil
}

// validateApplyDecision checks the object bytes pinned by plan_sha and the
// entire group partition before any child Apply can be admitted. Relational
// group and source checks must still run under the admission transaction.
func validateApplyDecision(raw []byte, req ApplyDecision, snapshotID uuid.UUID,
	snapshotDigest, policySHA string, itemCount int) (scenePlan, error) {
	invalid := func() (scenePlan, error) { return scenePlan{}, domain.NewError("VALIDATION_FAILED", 422) }
	if !refValid(req.Recipe) || len(req.ApprovedGroups) < 1 || len(req.ApprovedGroups) > 5000 {
		return invalid()
	}
	if _, err := revision(req.ExpectedRevision); err != nil {
		return invalid()
	}
	plan, err := decodeScenePlan(raw, req.PlanSHA, snapshotID, snapshotDigest,
		policySHA, itemCount)
	if err != nil {
		return scenePlan{}, err
	}
	groups := make(map[uuid.UUID]bool, len(plan.Groups))
	for _, group := range plan.Groups {
		groups[group.ID] = true
	}
	selected := make(map[uuid.UUID]bool, len(req.ApprovedGroups))
	for _, id := range req.ApprovedGroups {
		if id == uuid.Nil || selected[id] || !groups[id] {
			return invalid()
		}
		selected[id] = true
	}
	if _, err := projectApplySelection(plan, req.ApprovedGroups); err != nil {
		return invalid()
	}
	return plan, nil
}

func decodeScenePlan(raw []byte, planSHA string, snapshotID uuid.UUID,
	snapshotDigest, policySHA string, itemCount int) (scenePlan, error) {
	conflict := func() (scenePlan, error) { return scenePlan{}, domain.NewError("ANALYSIS_CONFLICT", 409) }
	if len(raw) == 0 || len(raw) > 8<<20 || !digestPattern.MatchString(planSHA) {
		return conflict()
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return conflict()
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != planSHA {
		return conflict()
	}
	var plan scenePlan
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&plan) != nil || decoder.Decode(new(any)) != io.EOF ||
		plan.SchemaVersion != 1 || plan.SnapshotID != snapshotID ||
		plan.SnapshotDigest != snapshotDigest || plan.PolicySHA != policySHA ||
		len(plan.Groups) < 1 || len(plan.Groups) > 5000 || itemCount < 1 || itemCount > 5000 {
		return conflict()
	}
	groups := make(map[uuid.UUID]bool, len(plan.Groups))
	ordinals := make(map[int]bool)
	for _, group := range plan.Groups {
		if group.ID == uuid.Nil || groups[group.ID] || len(group.Ordinals) < 1 ||
			len(group.Ordinals) > 5000 || !refValid(group.Corrections) ||
			math.IsNaN(group.Confidence) || group.Confidence < 0 || group.Confidence > 1 {
			return conflict()
		}
		groups[group.ID] = true
		referenceFound := false
		for _, ordinal := range group.Ordinals {
			if ordinal < 0 || ordinal > 4999 || ordinals[ordinal] {
				return conflict()
			}
			ordinals[ordinal] = true
			if ordinal == group.ReferenceOrdinal {
				referenceFound = true
			}
		}
		if !referenceFound {
			return conflict()
		}
	}
	if len(ordinals) != itemCount {
		return conflict()
	}
	for ordinal := 0; ordinal < itemCount; ordinal++ {
		if !ordinals[ordinal] {
			return conflict()
		}
	}
	return plan, nil
}

// ValidateSceneCorrectionEdge reuses the complete ScenePlan partition validator
// for immutable export provenance. SQL must independently prove producer scope.
func ValidateSceneCorrectionEdge(raw []byte, planSHA string, snapshotID uuid.UUID,
 snapshotDigest, policySHA string, itemCount int, groupID uuid.UUID,
 correction ArtifactRef, ordinals []int) error {
 if snapshotID==uuid.Nil||groupID==uuid.Nil||!refValid(correction)||len(ordinals)<1||len(ordinals)>5000 {
  return domain.NewError("ANALYSIS_CONFLICT",409)
 }
 plan,err:=decodeScenePlan(raw,planSHA,snapshotID,snapshotDigest,policySHA,itemCount);if err!=nil{return err}
 encoded,err:=canonical.Marshal(plan);if err!=nil||!bytes.Equal(encoded,raw){return domain.NewError("ANALYSIS_CONFLICT",409)}
 for _,group:=range plan.Groups{if group.ID==groupID{
  if group.Corrections!=correction||len(group.Ordinals)!=len(ordinals){return domain.NewError("ANALYSIS_CONFLICT",409)}
  for index,ordinal:=range ordinals{if group.Ordinals[index]!=ordinal{return domain.NewError("ANALYSIS_CONFLICT",409)}}
  return nil
 }}
 return domain.NewError("ANALYSIS_CONFLICT",409)
}
