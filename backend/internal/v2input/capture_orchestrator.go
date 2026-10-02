package v2input

import (
	"context"
	"errors"
	"time"
)

type CaptureAssessmentRepository interface {
	Claim(context.Context) (*CaptureAssessmentLease, error)
	Renew(context.Context, CaptureAssessmentLease) (bool, error)
	Release(context.Context, CaptureAssessmentLease, int) (bool, error)
	Fail(context.Context, CaptureAssessmentLease, string) (bool, error)
}

// CaptureAssessmentAdmission must freeze inputs before publication and atomically
// seal the graph and mark the job queued under the supplied lease token.
// Returning success without that transaction is a contract violation.
type CaptureAssessmentAdmission interface {
	AdmitCapture(context.Context, CaptureAssessmentLease) error
}

// CapturePermanentFailure contains only an allowlisted public error code.
// Detailed errors must never be persisted in the job's public error field.
var ErrCapturePending = errors.New("capture preparation pending")

type CapturePermanentFailure struct{ Code string }

func (e CapturePermanentFailure) Error() string { return e.Code }

type CaptureAssessmentOrchestrator struct {
	Store     CaptureAssessmentRepository
	Admission CaptureAssessmentAdmission
}

func (o CaptureAssessmentOrchestrator) ProcessOne(ctx context.Context) (bool, error) {
	if o.Store == nil || o.Admission == nil {
		return false, ErrInvalid
	}
	lease, err := o.Store.Claim(ctx)
	if err != nil || lease == nil {
		return false, err
	}
	if !validCaptureLease(*lease) {
		return false, ErrInvalid
	}
	work, cancel := context.WithCancel(ctx)
	defer cancel()
	// Object operations may outlive a lease interval. Renew independently and
	// cancel admission immediately on loss; its final SQL fence remains mandatory.
	stop := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				done <- nil
				return
			case <-work.Done():
				done <- work.Err()
				return
			case <-ticker.C:
				renewCtx, finish := context.WithTimeout(work, 5*time.Second)
				ok, e := o.Store.Renew(renewCtx, *lease)
				finish()
				if e != nil || !ok {
					if e == nil {
						e = ErrExpired
					}
					cancel()
					done <- e
					return
				}
			}
		}
	}()
	err = o.Admission.AdmitCapture(work, *lease)
	close(stop)
	heartbeatErr := <-done
	if err == nil && heartbeatErr == nil {
		return true, nil
	}
	if err == nil {
		err = heartbeatErr
	}
	cleanup, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finish()
	if errors.Is(err, ErrCapturePending) {
		released, releaseErr := o.Store.Release(cleanup, *lease, 2)
		if releaseErr != nil {
			return false, releaseErr
		}
		if !released {
			return false, ErrExpired
		}
		return true, nil
	}
	var permanent CapturePermanentFailure
	if errors.As(err, &permanent) && captureFailureCode.MatchString(permanent.Code) {
		_, cleanupErr := o.Store.Fail(cleanup, *lease, permanent.Code)
		return false, errors.Join(err, cleanupErr)
	}
	// Exponential backoff prevents an unavailable dependency from monopolizing
	// scheduler admission. Expired owners cannot alter a subsequent claim.
	delay := 2
	for i := 1; i < lease.Attempts && delay < 300; i++ {
		delay *= 2
	}
	if delay > 300 {
		delay = 300
	}
	_, cleanupErr := o.Store.Release(cleanup, *lease, delay)
	return false, errors.Join(err, cleanupErr)
}
