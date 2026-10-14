package run

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

func TestVerifiedOutputCopyBoundedAndCancellation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		body   string
		size   int64
		cancel bool
		ok     bool
	}{
		{"exact", "correct", 7, false, true},
		{"short", "short", 7, false, false},
		{"long", "too-long", 7, false, false},
		{"cancelled", "correct", 7, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			var sink bytes.Buffer
			wanted := sha256.New()
			err := verifiedOutputCopy(ctx, &sink, strings.NewReader(tc.body), tc.size, wanted)
			if tc.ok && (err != nil || sink.String() != tc.body) {
				t.Fatal("exact verified copy failed", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("invalid body accepted")
			}
			if tc.cancel && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}

func TestOutputGrantMatchesOnlyReviewableExactObject(t *testing.T) {
	grant := OutputGrant{Artifact: uuid.New(), Role: "working_image", SHA256: strings.Repeat("a", 64), ObjectKeySHA: outputObjectKeySHA("selected"), ObjectVersion: "v1", MediaType: "image/jpeg", Bytes: 20}
	item := pendingOutput{Output: Output{ArtifactID: grant.Artifact, Role: grant.Role, SHA256: grant.SHA256, MediaType: grant.MediaType, Bytes: grant.Bytes},
		Object: objectstore.Object{Key: "selected", VersionID: "v1"}, Revocable: true}
	if !outputGrantMatches(grant, item) {
		t.Fatal("exact reviewable output refused")
	}
	item.Revocable = false
	if outputGrantMatches(grant, item) {
		t.Fatal("ordinary output admitted by reviewable capability")
	}
	item.Revocable = true
	item.Object.VersionID = "v2"
	if outputGrantMatches(grant, item) {
		t.Fatal("substituted object version admitted")
	}
	item.Object.VersionID = "v1"
	item.Object.Key = "substituted"
	if outputGrantMatches(grant, item) {
		t.Fatal("substituted object key admitted")
	}
}
