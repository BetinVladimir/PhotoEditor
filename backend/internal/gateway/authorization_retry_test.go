package gateway

import (
	"context"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"testing"
	"time"
)

func TestGatewayAuthorizationSerializationRetryUsesFreshResult(t *testing.T) {
	ctx := context.WithValue(context.Background(), struct{}{}, "request")
	expected := TaskContext{Identity: TaskIdentity{TaskID: uuid.New()}, Operation: "semantic.transfer"}
	calls := 0
	got, err := retryGatewayAuthorization(ctx, func(actual context.Context) (TaskContext, error) {
		calls++
		if actual != ctx {
			t.Fatal("request context changed")
		}
		if calls < 3 {
			return TaskContext{Operation: "partial scan"}, fmt.Errorf("authorization: %w", &pgconn.PgError{Code: "40001"})
		}
		return expected, nil
	})
	if err != nil || calls != 3 || got.Identity != expected.Identity || got.Operation != expected.Operation {
		t.Fatal("fresh retry result", calls, got, err)
	}
}

func TestGatewayAuthorizationRetryBoundAndZeroFailureResult(t *testing.T) {
	failure := &pgconn.PgError{Code: "40001"}
	calls := 0
	got, err := retryGatewayAuthorization(context.Background(), func(context.Context) (TaskContext, error) {
		calls++
		return TaskContext{Operation: "partial scan"}, failure
	})
	if calls != 3 || err != failure || got.Operation != "" || got.Identity != (TaskIdentity{}) {
		t.Fatal("retry bound or partial result leak", calls, got, err)
	}
}

func TestGatewayAuthorizationPermanentErrorsNeverRetry(t *testing.T) {
	for _, failure := range []error{pgx.ErrNoRows, &pgconn.PgError{Code: "42501"}, &pgconn.PgError{Code: "55P03"}, errors.New("transport failure")} {
		calls := 0
		got, err := retryGatewayAuthorization(context.Background(), func(context.Context) (TaskContext, error) { calls++; return TaskContext{Operation: "partial"}, failure })
		if calls != 1 || err != failure || got.Operation != "" {
			t.Fatal("nonserialization retried", calls, err)
		}
	}
}

func TestGatewayAuthorizationCancellationStopsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	_, err := retryGatewayAuthorization(ctx, func(context.Context) (TaskContext, error) { calls++; return TaskContext{}, nil })
	if calls != 0 || err != context.Canceled {
		t.Fatal("cancelled request executed", calls, err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	_, err = retryGatewayAuthorization(ctx, func(context.Context) (TaskContext, error) {
		calls++
		cancel()
		return TaskContext{}, &pgconn.PgError{Code: "40001"}
	})
	if calls != 1 || err != context.Canceled {
		t.Fatal("cancelled query retried", calls, err)
	}
}

func TestGatewayAuthorizationDeadlineBoundsBackoff(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	calls := 0
	got, err := retryGatewayAuthorization(ctx, func(context.Context) (TaskContext, error) {
		calls++
		return TaskContext{Operation: "partial"}, &pgconn.PgError{Code: "40001"}
	})
	if calls > 1 || !errors.Is(err, context.DeadlineExceeded) || got.Operation != "" {
		t.Fatal("deadline ignored", calls, err)
	}
}
