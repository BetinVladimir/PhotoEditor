package collection

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestItemVersionReplaceStrictCAS(t *testing.T) {
	good := ItemVersionReplace{ExpectedRevision: "1", ExpectedVersion: uuid.New(), Version: uuid.New()}
	if err := validateItemVersionReplace(good); err != nil {
		t.Fatal(err)
	}
	for _, revision := range []string{"0", "01", "+1", "-1", "1.0", "9223372036854775808", ""} {
		bad := good
		bad.ExpectedRevision = revision
		if validateItemVersionReplace(bad) == nil {
			t.Fatal(revision)
		}
	}
	bad := good
	bad.Version = bad.ExpectedVersion
	if validateItemVersionReplace(bad) == nil {
		t.Fatal("same version")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (Service{}).ReplaceItemVersion(ctx, auth.Identity{}, uuid.New(), uuid.New(), uuid.New(), good); err != context.Canceled {
		t.Fatal(err)
	}
}
