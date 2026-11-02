package maintenance

import (
	"context"
	"testing"
)

func TestRequireEditorGCSchema_nilDB(t *testing.T) {
	if err := RequireEditorGCSchema(context.Background(), nil); err == nil {
		t.Fatal("nil DB must return error")
	}
}

func TestRequireResourceWithdrawal_cancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := RequireResourceWithdrawal(ctx, nil); err == nil {
		t.Fatal("cancelled context must return error")
	}
}
