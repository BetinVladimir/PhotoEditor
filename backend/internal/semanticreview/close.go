package semanticreview

import "errors"

var ErrReviewIncomplete = errors.New("semantic review has undecided targets")
var ErrConflict = errors.New("semantic review state or revision changed")

type ReviewClose struct {
	ExpectedRevision string `json:"expected_revision"`
	PlanSHA          string `json:"plan_sha"`
}

func DecodeReviewClose(raw []byte) (ReviewClose, error) {
	var result ReviewClose
	if decodeStrict(raw, &result) != nil || !validRevision(result.ExpectedRevision) || !shaPattern.MatchString(result.PlanSHA) {
		return ReviewClose{}, ErrInvalid
	}
	return result, nil
}

// ValidateClose checks server-owned counters under the eventual repository lock.
// Child completion is separate: all decisions must exist, not all children finish.
func ValidateClose(request ReviewClose, planSHA, revision, parentState string, remaining int) error {
	if !validRevision(request.ExpectedRevision) || !shaPattern.MatchString(request.PlanSHA) || remaining < 0 || remaining > 5000 {
		return ErrInvalid
	}
	if request.PlanSHA != planSHA || request.ExpectedRevision != revision || parentState != "waiting_review" {
		return ErrConflict
	}
	if remaining != 0 {
		return ErrReviewIncomplete
	}
	return nil
}
