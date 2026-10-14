package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestMultiPreviewIdentityBindsAttemptFenceResultAndICC(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			binding, task, _, _ := originalInventoryFixture(t, operation)
			source := MultiPreviewSource{Tenant: uuid.New(), Project: uuid.New(), Producer: task, Binding: binding, Attempt: 1, Fence: 2, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}}
			srgb := strings.Repeat("e", 64)
			base, err := MultiPreviewRunID(source, srgb)
			if err != nil {
				t.Fatal(err)
			}
			repeated, err := MultiPreviewRunID(source, srgb)
			if err != nil || repeated != base || base == binding.Run {
				t.Fatal("unstable preview identity")
			}
			for _, kind := range []string{"attempt", "fence", "result", "sha", "icc"} {
				changed := source
				profile := srgb
				switch kind {
				case "attempt":
					changed.Attempt++
				case "fence":
					changed.Fence++
				case "result":
					changed.Binding.ResultID = uuid.New()
				case "sha":
					changed.Binding.Result.SHA256 = strings.Repeat("f", 64)
				case "icc":
					profile = strings.Repeat("d", 64)
				}
				id, err := MultiPreviewRunID(changed, profile)
				if err != nil || id == base {
					t.Fatalf("%s omitted: %v", kind, err)
				}
			}
			for _, kind := range []string{"tenant", "project", "actor", "session", "attempt", "fence", "producer", "params", "icc"} {
				changed := source
				profile := srgb
				switch kind {
				case "tenant":
					changed.Tenant = uuid.Nil
				case "project":
					changed.Project = uuid.Nil
				case "actor":
					changed.Principal.UserID = uuid.Nil
				case "session":
					changed.Principal.SessionID = uuid.Nil
				case "attempt":
					changed.Attempt = 7
				case "fence":
					changed.Fence = 0
				case "producer":
					changed.Producer = uuid.New()
				case "params":
					changed.Binding.ParamsJCS = "{}"
				case "icc":
					profile = "invalid"
				}
				if _, err := MultiPreviewRunID(changed, profile); !errors.Is(err, v2input.ErrInvalid) {
					t.Fatalf("invalid %s: %v", kind, err)
				}
			}
		})
	}
}
func TestMultiPreviewSourcePreflightWithoutSQL(t *testing.T) {
	if _, err := (SQLMultiPreviewSource{}).Resolve(context.Background(), uuid.New(), uuid.New(), "optical.panorama"); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (SQLMultiPreviewSource{}).Resolve(ctx, uuid.Nil, uuid.Nil, ""); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
