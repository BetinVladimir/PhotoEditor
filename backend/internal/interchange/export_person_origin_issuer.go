package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/semanticproposal"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type PortablePersonOriginLocator interface {
	LookupPortablePersonOrigin(context.Context, v2input.SessionVersionBinding, v2input.Ref,
		v2input.Ref) (ExportSeriesMaskOrigin, error)
}

// Occurrence authority must traverse the selected immutable recipe and its
// exact Override/MaskRef edge. A catalog hash match is not such a proof.
type PortablePersonOriginOccurrence interface {
	QualifyPortablePersonOriginOccurrence(context.Context, PortablePersonOriginExpectation) error
}

type PortablePersonOriginSigner interface {
	SignPortablePersonOrigin(context.Context, []byte) ([]byte, error)
}

type PortablePersonOriginIssuerConfig struct {
	Locator    PortablePersonOriginLocator
	Reader     ExportQualifiedPersonReader
	Occurrence PortablePersonOriginOccurrence
	Signer     PortablePersonOriginSigner
	Who        auth.Identity
	Tenant     uuid.UUID
	IssuerID   uuid.UUID
	PublicKey  ed25519.PublicKey
	Report     v2input.Ref
	Expected   v2qualification.Expected
}

type PortablePersonOriginIssuer struct {
	locator    PortablePersonOriginLocator
	reader     ExportQualifiedPersonReader
	occurrence PortablePersonOriginOccurrence
	signer     PortablePersonOriginSigner
	who        auth.Identity
	tenant     uuid.UUID
	issuerID   uuid.UUID
	public     ed25519.PublicKey
	keyID      string
	report     v2input.Ref
	expected   []byte
}

func NewPortablePersonOriginIssuer(config PortablePersonOriginIssuerConfig) (*PortablePersonOriginIssuer, error) {
	e := config.Expected
	if config.Locator == nil || config.Reader == nil || config.Occurrence == nil || config.Signer == nil ||
		config.Tenant == uuid.Nil || config.IssuerID == uuid.Nil || config.Who.UserID == uuid.Nil ||
		config.Who.SessionID == uuid.Nil || len(config.PublicKey) != ed25519.PublicKeySize ||
		!validImportRef(config.Report) || e.Operation != "mask.person" || e.Version != "1" ||
		!e.LicenseCleared || (e.Backend != "cpu" && e.Backend != "mps" && e.Backend != "cuda") ||
		!portablePersonOriginSHA.MatchString(e.ModelLockSHA) {
		return nil, v2input.ErrInvalid
	}
	for _, digest := range []string{e.RuntimeLockSHA, e.DeviceProfileSHA, e.DatasetSHA} {
		if !portablePersonOriginSHA.MatchString(digest) {
			return nil, v2input.ErrInvalid
		}
	}
	frozen, err := canonical.Marshal(e)
	if err != nil {
		return nil, err
	}
	public := append(ed25519.PublicKey(nil), config.PublicKey...)
	digest := sha256.Sum256(public)
	return &PortablePersonOriginIssuer{locator: config.Locator, reader: config.Reader,
		occurrence: config.Occurrence, signer: config.Signer, who: config.Who,
		tenant: config.Tenant, issuerID: config.IssuerID, public: public,
		keyID: hex.EncodeToString(digest[:]), report: config.Report, expected: frozen}, nil
}

func (i *PortablePersonOriginIssuer) expectedPolicy() (v2qualification.Expected, error) {
	var expected v2qualification.Expected
	err := json.Unmarshal(i.expected, &expected)
	return expected, err
}

func validPersonOriginDocument(document ExportSeriesMaskDocument, limit int64) bool {
	object := document.Object
	return validImportRef(document.Ref) && object.Key != "" && object.VersionID != "" &&
		object.VersionID != "null" && object.SHA256 == document.Ref.SHA256 &&
		object.ContentType == "application/json" && object.Bytes >= 2 && object.Bytes <= limit
}

func validPersonOriginMask(object objectstore.Object, pixels v2input.Ref) bool {
	return object.Key != "" && object.VersionID != "" && object.VersionID != "null" &&
		object.SHA256 == pixels.SHA256 && object.Bytes > 0 &&
		(object.ContentType == "application/x-npy" || object.ContentType == "application/octet-stream")
}

func (i *PortablePersonOriginIssuer) Issue(ctx context.Context,
	scope PortablePersonOriginExpectation) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if i == nil || i.locator == nil || i.reader == nil || i.occurrence == nil || i.signer == nil ||
		scope.EntryID == uuid.Nil || scope.Source.AssetVersionID == uuid.Nil ||
		!validImportRef(scope.Source.Source) || !portablePersonOriginSHA.MatchString(scope.Source.CanvasHash) ||
		!validImportRef(scope.Recipe) || !validImportRef(scope.Override) ||
		!validImportRef(scope.MaskRef) || !validImportRef(scope.MaskPixels) {
		return nil, v2input.ErrInvalid
	}
	expected, err := i.expectedPolicy()
	if err != nil || scope.ModelLockSHA != expected.ModelLockSHA {
		return nil, v2input.ErrInvalid
	}
	if err := i.occurrence.QualifyPortablePersonOriginOccurrence(ctx, scope); err != nil {
		return nil, err
	}
	before, err := i.locator.LookupPortablePersonOrigin(ctx, scope.Source, scope.MaskRef, scope.MaskPixels)
	if err != nil {
		return nil, err
	}
	if before.Root.Ref != scope.MaskRef || before.Mask.SHA256 != scope.MaskPixels.SHA256 ||
		before.Task == uuid.Nil || before.Attempt < 1 || before.Fence < 1 ||
		before.Operation != "mask.person" || before.Version != "1" ||
		!validPersonOriginDocument(before.Root, 1<<20) ||
		!validPersonOriginDocument(before.Params, 65536) ||
		!validPersonOriginDocument(before.Report, 8<<20) ||
		!validPersonOriginMask(before.Mask, scope.MaskPixels) {
		return nil, v2input.ErrInvalid
	}
	mask, err := i.reader.ReadQualifiedPersonEvidence(ctx, i.who, i.tenant,
		scope.Source, scope.MaskRef, i.report, expected)
	if err != nil {
		return nil, err
	}
	if mask.Root != scope.MaskRef || mask.Pixels != scope.MaskPixels || rawSHA(mask.Body) != scope.MaskRef.SHA256 {
		return nil, v2input.ErrInvalid
	}
	typed, err := review.ParseMaskRef(mask.Body)
	if err != nil || typed.Selector != "person" || typed.ObjectID != nil || typed.ModelLockID == nil ||
		*typed.ModelLockID != expected.ModelLockSHA || typed.Artifact.ID != scope.MaskPixels.ArtifactID ||
		typed.Artifact.SHA256 != scope.MaskPixels.SHA256 {
		return nil, v2input.ErrInvalid
	}
	canvas, err := canonical.Hash(typed.Canvas)
	if err != nil || canvas != scope.Source.CanvasHash {
		return nil, v2input.ErrInvalid
	}
	if err := i.occurrence.QualifyPortablePersonOriginOccurrence(ctx, scope); err != nil {
		return nil, err
	}
	after, err := i.locator.LookupPortablePersonOrigin(ctx, scope.Source, scope.MaskRef, scope.MaskPixels)
	if err != nil || after != before {
		return nil, v2input.ErrInvalid
	}
	claim := PortablePersonOriginClaim{SchemaVersion: 1, IssuerID: i.issuerID,
		EntryID: scope.EntryID, Source: scope.Source, Recipe: scope.Recipe,
		Override: scope.Override, MaskRef: scope.MaskRef, MaskPixels: scope.MaskPixels,
		ProducerTaskID: before.Task, ProducerAttempt: before.Attempt, ProducerFence: before.Fence,
		Operation: "mask.person", OperationVersion: "1", ModelLockSHA: expected.ModelLockSHA,
		QualificationReport: i.report}
	body, err := canonical.Marshal(claim)
	if err != nil {
		return nil, err
	}
	signed := append([]byte(portablePersonOriginDomain), body...)
	signature, err := i.signer.SignPortablePersonOrigin(ctx, signed)
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(i.public, signed, signature) {
		return nil, errPortablePersonOrigin
	}
	if err := i.occurrence.QualifyPortablePersonOriginOccurrence(ctx, scope); err != nil {
		return nil, err
	}
	current, err := i.locator.LookupPortablePersonOrigin(ctx, scope.Source, scope.MaskRef, scope.MaskPixels)
	if err != nil || current != before {
		return nil, v2input.ErrInvalid
	}
	envelope := PortablePersonOriginEnvelope{SchemaVersion: 1, Claim: claim,
		KeyID: i.keyID, SignatureHex: hex.EncodeToString(signature)}
	raw, err := canonical.Marshal(envelope)
	if err != nil || len(raw) > 16384 {
		return nil, errPortablePersonOrigin
	}
	if _, err := VerifyPortablePersonOrigin(raw, scope,
		map[PortablePersonOriginKey]ed25519.PublicKey{{IssuerID: i.issuerID, KeyID: i.keyID}: i.public}); err != nil {
		return nil, err
	}
	return raw, ctx.Err()
}

var _ PortablePersonOriginLocator = SQLExportDocumentCatalog{}
var _ ExportQualifiedPersonReader = semanticproposal.SQLTransferSourceReader{}
