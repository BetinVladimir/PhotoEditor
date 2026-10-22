package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"testing"
)

func TestAuthoredGeometryContinuationTenantScope(t *testing.T) {
	id := uuid.New().String()
	for _, raw := range []string{"[]", "null", "[\"00000000-0000-0000-0000-000000000000\"]", "[\"" + id + "\",\"" + id + "\"]", "[\"" + id + "\"] trailing"} {
		if _, e := parseAuthoredGeometryContinuationTenants(raw); e == nil {
			t.Fatal("invalid tenant scope", raw)
		}
	}
	ids, e := parseAuthoredGeometryContinuationTenants("[\"" + id + "\"]")
	if e != nil || len(ids) != 1 || ids[0].String() != id {
		t.Fatal(e)
	}
}
func TestAuthoredGeometryContinuationCancellationBeforeDB(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, e := (*AuthoredGeometryContinuationProcessor)(nil).ProcessOne(ctx); !errors.Is(e, context.Canceled) {
		t.Fatal(e)
	}
}
