package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type decisionDocumentTarget struct {
	TargetID       uuid.UUID                     `json:"target_id"`
	TargetRevision string                        `json:"target_revision"`
	Decision       string                        `json:"decision"`
	Source         v2input.SessionVersionBinding `json:"source"`
	Candidate      v2input.Ref                   `json:"candidate"`
}
type decisionDocument struct {
	SchemaVersion    int                      `json:"schema_version"`
	Kind             string                   `json:"kind"`
	RunID            uuid.UUID                `json:"run_id"`
	ExpectedRevision string                   `json:"expected_revision"`
	Plan             v2input.Ref              `json:"plan"`
	Intent           v2input.Ref              `json:"intent"`
	SnapshotID       uuid.UUID                `json:"snapshot_id"`
	Targets          []decisionDocumentTarget `json:"targets"`
}
type DecisionProof struct {
	Body          []byte
	SHA256        string
	RequestSHA256 string
	Approved      []uuid.UUID
}

// BuildDecisionProof creates an immutable publication body from owned verified
// documents and repository-locked target identities. It neither publishes nor
// writes decisions. Admission must recheck every binding at the final commit.
func BuildDecisionProof(prepared PreparedReview, targets []LockedTarget, request ApplyDecision) (DecisionProof, error) {
	empty := DecisionProof{}
	owned, err := PrepareReviewDocuments(prepared.Sources, prepared.documents, prepared.modelSHA, prepared.datasetSHA)
	if err != nil {
		return empty, err
	}
	if len(targets) != len(owned.Targets) {
		return empty, ErrConflict
	}
	seeds := make(map[uuid.UUID]InitialTarget, len(owned.Targets))
	for _, seed := range owned.Targets {
		seeds[seed.AssetVersionID] = seed
	}
	states := make(map[uuid.UUID]string, len(targets))
	byID := make(map[uuid.UUID]LockedTarget, len(targets))
	versions := make(map[uuid.UUID]bool, len(targets))
	for _, target := range targets {
		seed, ok := seeds[target.AssetVersionID]
		if !ok || target.ID == uuid.Nil || target.Revision < 1 || !validTargetState(target.State) || target.CandidateArtifactID == nil || *target.CandidateArtifactID != seed.CandidateArtifactID || versions[target.AssetVersionID] {
			return empty, ErrConflict
		}
		if _, duplicate := byID[target.ID]; duplicate {
			return empty, ErrConflict
		}
		states[target.ID] = target.State
		byID[target.ID] = target
		versions[target.AssetVersionID] = true
	}
	selected, err := ValidateSelection(request, owned.PlanRef.SHA256, strconv.FormatInt(owned.Sources.Revision, 10), states)
	if err != nil {
		return empty, err
	}
	bindings := make(map[uuid.UUID]v2input.SessionVersionBinding, len(targets))
	refs := make(map[uuid.UUID]v2input.Ref, len(targets))
	for _, document := range owned.documents {
		if document.Source.Contract != "TransferCandidate" {
			continue
		}
		candidate, err := v2input.DecodeSemanticCandidate(document.Body)
		if err != nil {
			return empty, ErrInvalid
		}
		bindings[document.Source.Ref.ArtifactID] = candidate.Target
		refs[document.Source.Ref.ArtifactID] = document.Source.Ref
	}
	doc := decisionDocument{SchemaVersion: 1, Kind: "semantic_review", RunID: owned.Sources.RunID, ExpectedRevision: request.ExpectedRevision, Plan: owned.PlanRef, Intent: owned.IntentRef, SnapshotID: owned.SnapshotID, Targets: make([]decisionDocumentTarget, 0, len(selected))}
	proof := DecisionProof{}
	for _, selection := range selected {
		target := byID[selection.TargetID]
		if seeds[target.AssetVersionID].State != "proposed" || target.Revision != 1 || target.ChildRunID != nil || target.BatchID != nil {
			return empty, ErrConflict
		}
		artifact := *target.CandidateArtifactID
		doc.Targets = append(doc.Targets, decisionDocumentTarget{TargetID: target.ID, TargetRevision: strconv.FormatInt(target.Revision, 10), Decision: selection.Decision, Source: bindings[artifact], Candidate: refs[artifact]})
		if selection.Decision == "approve" {
			proof.Approved = append(proof.Approved, target.ID)
		}
	}
	proof.Body, err = canonical.Marshal(doc)
	if err != nil {
		return empty, err
	}
	sum := sha256.Sum256(proof.Body)
	proof.SHA256 = hex.EncodeToString(sum[:])
	proof.RequestSHA256, err = HashApplyRequest(owned.Sources.RunID, request)
	if err != nil {
		return empty, err
	}
	return proof, nil
}
