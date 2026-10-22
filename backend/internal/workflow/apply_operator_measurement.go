package workflow

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"
)

// OperatorMeasuredTask is a measurement observation, never a task_results row.
// The compute port must retain actual owned bytes until AcceptMeasuredTask has
// validated the operation's current typed outputs. No qualified installation or
// accepted SQL receipt is created by this operator-only driver.
type OperatorMeasuredTask struct {
	Task              PreparedTask
	Elapsed           time.Duration
	OutputBytes       int64
	ObservationSHA256 string
	ObservationRaw    json.RawMessage
	Outputs           []OperatorMeasuredOutput
}

// OperatorApplyStore runs the same phase compiler against an isolated measured
// object ledger. Review methods inherited from ApplyPhaseStore must refuse an
// absent or mismatched explicit operator decision, never auto-accept a barrier.
type OperatorApplyStore interface {
	ApplyPhaseStore
	PendingMeasuredTasks(context.Context) ([]PreparedTask, error)
	AcceptMeasuredTask(context.Context, PreparedTask, OperatorMeasuredTask) error
	MeasurementComplete(context.Context) (bool, error)
}
type OperatorApplyCompute interface {
	// Execute invokes the production stage factory/lower compute with the exact
	// Params/input/profile; it is not permitted to manufacture result receipts.
	Execute(context.Context, PreparedTask) (OperatorMeasuredTask, error)
}

// MeasureOperatorApply preserves production phase order/Params by calling
// ProcessOne, rather than interpreting module labels or inventing a new DAG.
// It deliberately returns observations, not a passed qualification Report.
func MeasureOperatorApply(ctx context.Context, c ApplyCoordinator, store OperatorApplyStore, compute OperatorApplyCompute, maxTicks int) ([]OperatorMeasuredTask, error) {
	if store == nil || compute == nil || maxTicks < 1 || maxTicks > 100000 {
		return nil, errors.New("operator staged measurement configuration invalid")
	}
	if _, bounded := ctx.Deadline(); !bounded {
		return nil, errors.New("operator measurement deadline required")
	}
	c.Store = store
	observations := []OperatorMeasuredTask{}
	seen := map[string]bool{}
	for tick := 0; tick < maxTicks; tick++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		done, err := store.MeasurementComplete(ctx)
		if err != nil {
			return nil, err
		}
		if done {
			if len(observations) == 0 {
				return nil, errors.New("empty staged measurement")
			}
			return observations, nil
		}
		changed, err := c.ProcessOne(ctx)
		if errors.Is(err, errOperatorApprovalPending) {
			timer := time.NewTimer(100 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if err != nil {
			return nil, err
		}
		tasks, err := store.PendingMeasuredTasks(ctx)
		if err != nil {
			return nil, err
		}
		for _, task := range tasks {
			identity := task.ID.String()
			if seen[identity] {
				return nil, errors.New("duplicate staged measurement task")
			}
			if task.TimeoutSeconds < 1 {
				return nil, errors.New("stage timeout required")
			}
			taskCtx, cancel := context.WithTimeout(ctx, time.Duration(task.TimeoutSeconds)*time.Second)
			began := time.Now()
			observed, executeErr := compute.Execute(taskCtx, task)
			elapsed := time.Since(began)
			deadlineErr := taskCtx.Err()
			cancel()
			if executeErr != nil {
				return nil, executeErr
			}
			if deadlineErr != nil {
				return nil, deadlineErr
			}
			// The measured wall-clock interval is owned by the driver, never supplied
			// by an adapter or copied from a golden/report fixture.
			observed.Elapsed = elapsed
			if !reflect.DeepEqual(observed.Task, task) || observed.OutputBytes < 1 || len(observed.ObservationSHA256) != 64 || strings.ToLower(observed.ObservationSHA256) != observed.ObservationSHA256 {
				return nil, errors.New("staged measurement observation binding invalid")
			}
			if _, e := hex.DecodeString(observed.ObservationSHA256); e != nil {
				return nil, errors.New("invalid observation hash")
			}
			if err = store.AcceptMeasuredTask(ctx, task, observed); err != nil {
				return nil, err
			}
			seen[identity] = true
			observations = append(observations, observed)
		}
		if !changed && len(tasks) == 0 {
			return nil, errors.New("staged measurement blocked: explicit operator decision or required input absent")
		}
	}
	return nil, errors.New("operator staged measurement tick bound exceeded")
}
