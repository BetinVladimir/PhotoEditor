package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/semanticproposal"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type personOriginLocatorStub struct {
	origin ExportSeriesMaskOrigin
	calls  int
	change bool
}

func (s *personOriginLocatorStub) LookupPortablePersonOrigin(_ context.Context,
	_ v2input.SessionVersionBinding, _, _ v2input.Ref) (ExportSeriesMaskOrigin, error) {
	s.calls++
	out := s.origin
	if s.change && s.calls > 1 {
		out.Fence++
	}
	return out, nil
}

type personOriginOccurrenceStub struct {
	calls int
	deny  bool
}

func (s *personOriginOccurrenceStub) QualifyPortablePersonOriginOccurrence(_ context.Context,
	_ PortablePersonOriginExpectation, _ []byte) error {
	s.calls++
	if s.deny {
		return v2input.ErrInvalid
	}
	return nil
}

type personOriginSignerStub struct {
	key   ed25519.PrivateKey
	wrong bool
	calls int
}

func (s *personOriginSignerStub) SignPortablePersonOrigin(_ context.Context, body []byte) ([]byte, error) {
	s.calls++
	if s.wrong {
		return make([]byte, ed25519.SignatureSize), nil
	}
	return ed25519.Sign(s.key, body), nil
}

type personOriginReaderStub struct {
	mask  semanticproposal.VerifiedPersonMask
	calls int
	deny  bool
}

func (s *personOriginReaderStub) ReadQualifiedPersonEvidence(_ context.Context, _ auth.Identity,
	_ uuid.UUID, _ v2input.SessionVersionBinding, _, _ v2input.Ref,
	_ v2qualification.Expected) (semanticproposal.VerifiedPersonMask, error) {
	s.calls++
	if s.deny {
		return semanticproposal.VerifiedPersonMask{}, v2qualification.ErrUnqualified
	}
	return s.mask, nil
}

func personOriginIssuerFixture(t *testing.T) (*PortablePersonOriginIssuer,
	PortablePersonOriginExpectation, *personOriginLocatorStub, *personOriginOccurrenceStub,
	*personOriginSignerStub, *personOriginReaderStub) {
	t.Helper()
	_, entry, body := importWorkingValidatorFixture(t)
	image, err := review.ParseWorkingImage(body)
	if err != nil {
		t.Fatal(err)
	}
	canvas, err := canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	model := rawSHA([]byte("model"))
	mask := review.MaskRef{Artifact: image.Pixels, Canvas: image.Canvas,
		TransformChain: image.TransformChain, Selector: "person", Confidence: .9,
		ModelLockID: &model}
	rootBody, err := canonical.Marshal(mask)
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(rootBody)}
	pixels := v2input.Ref{ArtifactID: image.Pixels.ID, SHA256: image.Pixels.SHA256}
	scope := PortablePersonOriginExpectation{EntryID: uuid.New(),
		Source: v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: entry.Ref, CanvasHash: canvas},
		Recipe:   v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))},
		Override: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("override"))},
		MaskRef:  root, MaskPixels: pixels, ModelLockSHA: model}
	locator := &personOriginLocatorStub{origin: ExportSeriesMaskOrigin{Task: uuid.New(),
		Operation: "mask.person", Version: "1", Attempt: 2, Fence: 3,
		Root:   ExportSeriesMaskDocument{Ref: root, Object: objectstore.Object{Key: "root", VersionID: "v1", SHA256: root.SHA256, Bytes: int64(len(rootBody)), ContentType: "application/json"}},
		Params: ExportSeriesMaskDocument{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("params"))}},
		Report: ExportSeriesMaskDocument{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("report"))}}}}
	locator.origin.Params.Object = objectstore.Object{Key: "params", VersionID: "v1", SHA256: locator.origin.Params.Ref.SHA256, Bytes: 6, ContentType: "application/json"}
	locator.origin.Report.Object = objectstore.Object{Key: "report", VersionID: "v1", SHA256: locator.origin.Report.Ref.SHA256, Bytes: 6, ContentType: "application/json"}
	locator.origin.Mask = objectstore.Object{Key: "pixels", VersionID: "v1", SHA256: pixels.SHA256, Bytes: 16, ContentType: "application/x-npy"}
	occurrence := &personOriginOccurrenceStub{}
	reader := &personOriginReaderStub{mask: semanticproposal.VerifiedPersonMask{Root: root, Pixels: pixels, Body: rootBody}}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer := &personOriginSignerStub{key: private}
	expected := v2qualification.Expected{Operation: "mask.person", Version: "1", Backend: "mps",
		ModelLockSHA: model, RuntimeLockSHA: model, DeviceProfileSHA: model,
		DatasetSHA: model, LicenseCleared: true, RequiredChecks: []string{"coverage"}}
	issuer, err := NewPortablePersonOriginIssuer(PortablePersonOriginIssuerConfig{
		Locator: locator, Reader: reader, Occurrence: occurrence, Signer: signer,
		Who: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, Tenant: uuid.New(),
		IssuerID: uuid.New(), PublicKey: public,
		Report:   v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("qualification"))},
		Expected: expected})
	if err != nil {
		t.Fatal(err)
	}
	return issuer, scope, locator, occurrence, signer, reader
}

func TestPortablePersonOriginIssuerRequiresPhysicalOriginAndStableAuthority(t *testing.T) {
	for _, mode := range []string{"valid", "occurrence", "locator", "reader", "signer", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			issuer, scope, locator, occurrence, signer, reader := personOriginIssuerFixture(t)
			switch mode {
			case "occurrence":
				occurrence.deny = true
			case "locator":
				locator.change = true
			case "reader":
				reader.deny = true
			case "signer":
				signer.wrong = true
			}
			ctx := context.Background()
			if mode == "cancelled" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			raw, err := issuer.Issue(ctx, scope)
			if mode == "valid" {
				if err != nil || len(raw) < 2 || locator.calls != 3 || occurrence.calls != 2 ||
					reader.calls != 1 || signer.calls != 1 {
					t.Fatalf("valid issue failed: %v", err)
				}
			} else if err == nil || len(raw) != 0 {
				t.Fatal("unqualified origin signed", mode)
			}
			if mode == "cancelled" && !errors.Is(err, context.Canceled) {
				t.Fatal("cancellation lost", err)
			}
		})
	}
}
