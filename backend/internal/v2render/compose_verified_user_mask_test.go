package v2render

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type verifiedUserPixels struct {
	fixture *maskVerifyFixture
	calls   int
	binds   int
	denied  bool
}

func (p *verifiedUserPixels) LookupUserMaskPixels(ctx context.Context, who auth.Identity, tid, version uuid.UUID, ref v2input.Ref, w, h int64) (objectstore.Object, error) {
	p.calls++
	if p.denied {
		return objectstore.Object{}, v2input.ErrExpired
	}
	if version == uuid.Nil || w != 400 || h != 300 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return p.fixture.Lookup(ctx, who, tid, v2draft.LayerRef{Ref: ref, Contract: "MaskPixels"})
}
func TestVerifiedNewUserMaskPublication(t *testing.T) {
	for _, tc := range []struct {
		name            string
		invalid, denied bool
	}{
		{name: "valid"}, {name: "invalid_probability", invalid: true}, {name: "binary_denied", denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, base, mask := maskVerificationFixture(t, tc.invalid)
			pixels := &verifiedUserPixels{fixture: fixture, denied: tc.denied}
			calls := 0
			publisher := compositionPublisherFunc(func(_ context.Context, _ auth.Identity, _, _, _ uuid.UUID, body []byte) (v2input.Ref, error) {
				calls++
				return v2input.Ref{ArtifactID: uuid.New(), SHA256: maskSHA(body)}, nil
			})
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			_, err := PublishVerifiedUserCompositionMask(context.Background(), who, uuid.New(), uuid.New(), base, fixture.bodies[mask.ArtifactID.String()], fixture, pixels, fixture, publisher)
			if tc.denied {
				if !errors.Is(err, v2input.ErrExpired) || calls != 0 || fixture.opens != 1 || fixture.closes != 1 {
					t.Fatal("denied binary publication", err, calls, fixture.opens, fixture.closes)
				}
			} else if tc.invalid {
				if !errors.Is(err, v2input.ErrInvalid) || calls != 0 || fixture.opens != 2 || fixture.closes != 2 {
					t.Fatal("invalid probability published", err, calls)
				}
			} else if err != nil || calls != 1 || fixture.opens != 2 || fixture.closes != 2 {
				t.Fatal("valid publication", err, calls, fixture.opens, fixture.closes)
			}
			if pixels.binds != calls {
				t.Fatal("binding count", pixels.binds, calls)
			}
			if pixels.calls != 1 {
				t.Fatal("binary version scope not checked")
			}
		})
	}
}

func (p *verifiedUserPixels) BindUserMask(_ context.Context, _ auth.Identity, _ uuid.UUID, _ v2input.SessionVersionBinding, root v2input.Ref, raw []byte) error {
	p.binds++
	if maskSHA(raw) != root.SHA256 {
		return v2input.ErrInvalid
	}
	return nil
}
