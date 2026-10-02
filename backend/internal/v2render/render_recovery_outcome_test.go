package v2render

import (
	"context"
	"errors"
	"io"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRecoveryReadinessKeepsInfrastructureAndUnrecordedFailures(t *testing.T) {
	rejection := renderRecoveryInputError{v2input.ErrExpired}
	for _, tc := range []struct {
		name     string
		err      error
		recorded bool
		update   error
		want     []error
	}{
		{"recorded input rejection", rejection, true, nil, nil},
		{"unrecorded input rejection", rejection, false, nil, []error{v2input.ErrExpired}},
		{"lock cleanup joined to rejection", errors.Join(rejection, io.ErrUnexpectedEOF), true, nil, []error{v2input.ErrExpired, io.ErrUnexpectedEOF}},
		{"schedule write failed", rejection, false, io.ErrUnexpectedEOF, []error{v2input.ErrExpired, io.ErrUnexpectedEOF}},
		{"factory invalid configuration", v2input.ErrInvalid, true, nil, []error{v2input.ErrInvalid}},
		{"caller cancellation", context.Canceled, true, nil, []error{context.Canceled}},
		{"transport unavailable", io.ErrUnexpectedEOF, true, nil, []error{io.ErrUnexpectedEOF}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			done, err := renderRecoveryOutcome(false, tc.err, tc.recorded, tc.update)
			if done {
				t.Fatal("failed recovery counted as completed")
			}
			if len(tc.want) == 0 {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			for _, expected := range tc.want {
				if !errors.Is(err, expected) {
					t.Fatal("failure evidence lost", err, expected)
				}
			}
		})
	}
	done, err := renderRecoveryOutcome(true, nil, false, io.ErrUnexpectedEOF)
	if !done || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal("committed recovery progress lost", done, err)
	}
}
