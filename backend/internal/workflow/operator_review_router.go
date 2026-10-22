package workflow

import (
	"context"
	"errors"
)

// OperatorReviewRouter has no fallback acceptance. Each actual barrier must
// have an explicitly configured production-rule materializer.
type OperatorReviewRouter map[string]OperatorReviewMaterializer

func (r OperatorReviewRouter) MaterializeOperatorReview(ctx context.Context, l ApplyLease, inputs []applyInput, outputs []OperatorMeasuredOutput) (OperatorReviewResult, error) {
	port, ok := r[l.Phase]
	if !ok || port == nil {
		return OperatorReviewResult{}, errors.New("explicit operator phase review port required")
	}
	return port.MaterializeOperatorReview(ctx, l, inputs, outputs)
}

var _ OperatorReviewMaterializer = OperatorReviewRouter{}
