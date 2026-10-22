package exportjob

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
)

func TestGetRejectsMissingDependenciesAndIdentifiers(t *testing.T) {
	s := Service{}
	if _, err := s.Get(context.Background(), auth.Identity{UserID: uuid.New()}, uuid.New(), uuid.New()); domain.AsError(err).Code != "VALIDATION_FAILED" {
		t.Fatalf("unexpected %v", err)
	}
}

func TestTerminalCodeIsStrictAndBounded(t *testing.T) {
	for _, value := range []string{"FAILED", "RESOURCE_OOM", "X1"} {
		if !terminalCode.MatchString(value) {
			t.Fatal(value)
		}
	}
	for _, value := range []string{"", "failed", "secret: value", string(make([]byte, 81))} {
		if terminalCode.MatchString(value) {
			t.Fatal(value)
		}
	}
}

func TestCreateRejectsMissingDependenciesBeforeTenantTransaction(t *testing.T){
 _,err:=(Service{}).Create(context.Background(),auth.Identity{UserID:uuid.New()},uuid.New(),"key",CreateRequest{SnapshotID:uuid.New(),Presets:[]uuid.UUID{uuid.New()}})
 if domain.AsError(err).Code!="VALIDATION_FAILED"{t.Fatal(err)}
}
