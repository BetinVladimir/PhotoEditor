package semanticreview

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

// CloseProof is prepared from repository-owned state. The eventual commit must
// lock the parent and targets and repeat these checks before publishing success.
type CloseProof struct {
	Body          []byte
	SHA256        string
	RequestSHA256 string
}

func HashCloseRequest(parentRun uuid.UUID, request ReviewClose) (string, error) {
	if parentRun == uuid.Nil {
		return "", ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return "", ErrInvalid
	}
	verified, err := DecodeReviewClose(raw)
	if err != nil {
		return "", err
	}
	return canonical.Hash(struct {
		RunID   uuid.UUID   `json:"run_id"`
		Request ReviewClose `json:"request"`
	}{parentRun, verified})
}

// BuildCloseProof counts undecided targets rather than unfinished child tasks.
// queued/applied/failed/cancelled targets already have an admitted decision.
func BuildCloseProof(parentRun uuid.UUID, request ReviewClose, planSHA, revision, parentState string, targets []LockedTarget) (CloseProof, error) {
	if parentRun == uuid.Nil || len(targets) == 0 || len(targets) > 5000 {
		return CloseProof{}, ErrInvalid
	}
	seen := make(map[uuid.UUID]bool, len(targets))
	remaining := 0
	for _, target := range targets {
		if target.ID == uuid.Nil || seen[target.ID] || !validTargetState(target.State) {
			return CloseProof{}, ErrInvalid
		}
		seen[target.ID] = true
		if target.State == "proposed" {
			remaining++
		}
	}
	if err := ValidateClose(request, planSHA, revision, parentState, remaining); err != nil {
		return CloseProof{}, err
	}
	requestSHA, err := HashCloseRequest(parentRun, request)
	if err != nil {
		return CloseProof{}, err
	}
	document := struct {
		SchemaVersion    int       `json:"schema_version"`
		Kind             string    `json:"kind"`
		RunID            uuid.UUID `json:"run_id"`
		ExpectedRevision string    `json:"expected_revision"`
		PlanSHA          string    `json:"plan_sha"`
	}{1, "semantic_review_close", parentRun, request.ExpectedRevision, request.PlanSHA}
	body, err := canonical.Marshal(document)
	if err != nil {
		return CloseProof{}, err
	}
	sha, err := canonical.Hash(document)
	if err != nil {
		return CloseProof{}, err
	}
	return CloseProof{Body: body, SHA256: sha, RequestSHA256: requestSHA}, nil
}
