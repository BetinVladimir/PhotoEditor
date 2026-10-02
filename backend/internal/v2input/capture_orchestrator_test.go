package v2input

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

type captureRepoFake struct {
	lease *CaptureAssessmentLease
	delay int
	code  string
}

func (s *captureRepoFake) Claim(context.Context) (*CaptureAssessmentLease, error) {
	return s.lease, nil
}
func (s *captureRepoFake) Renew(context.Context, CaptureAssessmentLease) (bool, error) {
	return true, nil
}
func (s *captureRepoFake) Release(_ context.Context, _ CaptureAssessmentLease, d int) (bool, error) {
	s.delay = d
	return true, nil
}
func (s *captureRepoFake) Fail(_ context.Context, _ CaptureAssessmentLease, c string) (bool, error) {
	s.code = c
	return true, nil
}

type captureAdmissionFunc func(context.Context, CaptureAssessmentLease) error

func (f captureAdmissionFunc) AdmitCapture(c context.Context, l CaptureAssessmentLease) error {
	return f(c, l)
}
func TestCaptureAdmissionOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		err   error
		delay int
		code  string
		ok    bool
	}{
		{"success", nil, 0, "", true},
		{"pending", ErrCapturePending, 2, "", true}, {"retry", errors.New("object unavailable"), 300, "", false},
		{"permanent", CapturePermanentFailure{"CAPTURE_INPUT_INVALID"}, 0, "CAPTURE_INPUT_INVALID", false},
		{"unsafe code", CapturePermanentFailure{"secret details"}, 300, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			l := &CaptureAssessmentLease{JobID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ProjectID: uuid.New(), ReceiptID: uuid.New(), SessionID: uuid.New(), AssetVersionID: uuid.New(), Until: time.Now().Add(time.Minute), CreatedAt: time.Now(), Attempts: 12}
			repo := &captureRepoFake{lease: l}
			admission := captureAdmissionFunc(func(_ context.Context, v CaptureAssessmentLease) error {
				if v.Token != l.Token {
					t.Fatal("fence changed")
				}
				return tc.err
			})
			ok, err := (CaptureAssessmentOrchestrator{repo, admission}).ProcessOne(context.Background())
			if ok != tc.ok || (err == nil) != tc.ok || repo.delay != tc.delay || repo.code != tc.code {
				t.Fatalf("%v %v %+v", ok, err, repo)
			}
		})
	}
}
func TestCaptureEmptyQueue(t *testing.T) {
	repo := &captureRepoFake{}
	admission := captureAdmissionFunc(func(context.Context, CaptureAssessmentLease) error { t.Fatal("empty queue admitted"); return nil })
	ok, err := (CaptureAssessmentOrchestrator{repo, admission}).ProcessOne(context.Background())
	if ok || err != nil {
		t.Fatalf("%v %v", ok, err)
	}
}
