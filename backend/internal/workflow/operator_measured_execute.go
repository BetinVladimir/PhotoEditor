package workflow

import (
	"context"
	"time"
)

// executeMeasuredOperator owns wall duration, never child-reported timing.
// A late nil success is rejected before any measured task can be accepted.
func executeMeasuredOperator(ctx context.Context, task PreparedTask, execute func(context.Context, PreparedTask) (OperatorMeasuredTask, error)) (OperatorMeasuredTask, error) {
	began := time.Now()
	observation, err := execute(ctx, task)
	elapsed := time.Since(began)
	if err != nil {
		return OperatorMeasuredTask{}, err
	}
	if err = ctx.Err(); err != nil {
		return OperatorMeasuredTask{}, err
	}
	observation.Elapsed = elapsed
	return observation, nil
}
