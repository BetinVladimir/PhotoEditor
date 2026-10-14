package delivery

import (
	"context"
	"errors"
	"testing"
)

func TestDelegatedSigningExecutionReaderRequiresScopeAndPolicies(t *testing.T) {
	reader, err := NewDelegatedSigningExecutionReader(SQLDelegatedTaskCatalog{}, nil, nil, "")
	if err == nil || reader.Claims != nil { t.Fatal("unconfigured execution reader installed") }
}

func TestCurrentDelegatedLineageRequiresRuntime(t *testing.T) {
	if _, _, err := CompileAndPublishCurrentDelegatedLineage(context.Background(),
		SQLDelegatedLineageStore{}, nil, nil, nil, ""); err == nil {
		t.Fatal("unconfigured lineage runtime accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := CompileAndPublishCurrentDelegatedLineage(ctx,
		SQLDelegatedLineageStore{}, nil, nil, nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation ignored", err)
	}
}
