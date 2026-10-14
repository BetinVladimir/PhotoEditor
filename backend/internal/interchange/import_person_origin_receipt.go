package interchange

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

const portablePersonOriginDomain = "PhotoEditor/V30/person-mask-origin/v1\x00"

var portablePersonOriginSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errPortablePersonOrigin = errors.New("unqualified portable person-mask origin")

// This claim is an issuer assertion, not user consent. The exporter must sign
// only after independently qualifying the original mask.person producer.
type PortablePersonOriginClaim struct {
	SchemaVersion       int                           `json:"schema_version"`
	IssuerID            uuid.UUID                     `json:"issuer_id"`
	EntryID             uuid.UUID                     `json:"entry_id"`
	Source              v2input.SessionVersionBinding `json:"source"`
	Recipe              v2input.Ref                   `json:"recipe"`
	Override            v2input.Ref                   `json:"override"`
	MaskRef             v2input.Ref                   `json:"mask_ref"`
	MaskPixels          v2input.Ref                   `json:"mask_pixels"`
	ProducerTaskID      uuid.UUID                     `json:"producer_task_id"`
	ProducerAttempt     int                           `json:"producer_attempt"`
	ProducerFence       int64                         `json:"producer_fence"`
	Operation           string                        `json:"operation"`
	OperationVersion    string                        `json:"operation_version"`
	ModelLockSHA        string                        `json:"model_lock_sha"`
	QualificationReport v2input.Ref                   `json:"qualification_report"`
}

type PortablePersonOriginEnvelope struct {
	SchemaVersion int                       `json:"schema_version"`
	Claim         PortablePersonOriginClaim `json:"claim"`
	KeyID         string                    `json:"key_id"`
	SignatureHex  string                    `json:"signature_hex"`
}

type PortablePersonOriginExpectation struct {
	EntryID      uuid.UUID
	Source       v2input.SessionVersionBinding
	Recipe       v2input.Ref
	Override     v2input.Ref
	MaskRef      v2input.Ref
	MaskPixels   v2input.Ref
	ModelLockSHA string
}

type PortablePersonOriginKey struct {
	IssuerID uuid.UUID
	KeyID    string
}

func validPersonOriginRef(ref v2input.Ref) bool {
	return ref.ArtifactID != uuid.Nil && portablePersonOriginSHA.MatchString(ref.SHA256)
}

func validPersonOriginClaim(claim PortablePersonOriginClaim) bool {
	return claim.SchemaVersion == 1 && claim.IssuerID != uuid.Nil && claim.EntryID != uuid.Nil &&
		claim.Source.AssetVersionID != uuid.Nil && validPersonOriginRef(claim.Source.Source) &&
		portablePersonOriginSHA.MatchString(claim.Source.CanvasHash) &&
		validPersonOriginRef(claim.Recipe) && validPersonOriginRef(claim.Override) &&
		validPersonOriginRef(claim.MaskRef) && validPersonOriginRef(claim.MaskPixels) &&
		claim.ProducerTaskID != uuid.Nil && claim.ProducerAttempt >= 1 && claim.ProducerFence >= 1 &&
		claim.Operation == "mask.person" && claim.OperationVersion == "1" &&
		portablePersonOriginSHA.MatchString(claim.ModelLockSHA) &&
		validPersonOriginRef(claim.QualificationReport)
}

// VerifyPortablePersonOrigin uses only trusted process-provisioned public keys
// and caller-owned expected refs. Bundle inventory and request JSON are never
// accepted as trust or scope. A separate current user grant remains mandatory.
func VerifyPortablePersonOrigin(raw []byte, expected PortablePersonOriginExpectation,
	trusted map[PortablePersonOriginKey]ed25519.PublicKey,
) (PortablePersonOriginClaim, error) {
	var zero PortablePersonOriginClaim
	if len(raw) < 2 || len(raw) > 16384 || trusted == nil {
		return zero, errPortablePersonOrigin
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return zero, errPortablePersonOrigin
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var envelope PortablePersonOriginEnvelope
	if decoder.Decode(&envelope) != nil || decoder.Decode(new(any)) != io.EOF {
		return zero, errPortablePersonOrigin
	}
	reencoded, err := canonical.Marshal(envelope)
	if err != nil || !bytes.Equal(reencoded, raw) || envelope.SchemaVersion != 1 ||
		!validPersonOriginClaim(envelope.Claim) || !portablePersonOriginSHA.MatchString(envelope.KeyID) ||
		envelope.Claim.EntryID != expected.EntryID || envelope.Claim.Source != expected.Source || envelope.Claim.Recipe != expected.Recipe ||
		envelope.Claim.Override != expected.Override || envelope.Claim.MaskRef != expected.MaskRef ||
		envelope.Claim.MaskPixels != expected.MaskPixels || envelope.Claim.ModelLockSHA != expected.ModelLockSHA {
		return zero, errPortablePersonOrigin
	}
	signature, err := hex.DecodeString(envelope.SignatureHex)
	if err != nil || len(signature) != ed25519.SignatureSize || hex.EncodeToString(signature) != envelope.SignatureHex {
		return zero, errPortablePersonOrigin
	}
	public := trusted[PortablePersonOriginKey{IssuerID: envelope.Claim.IssuerID, KeyID: envelope.KeyID}]
	keyDigest := sha256.Sum256(public)
	if len(public) != ed25519.PublicKeySize || hex.EncodeToString(keyDigest[:]) != envelope.KeyID {
		return zero, errPortablePersonOrigin
	}
	claimRaw, err := canonical.Marshal(envelope.Claim)
	if err != nil || !ed25519.Verify(public, append([]byte(portablePersonOriginDomain), claimRaw...), signature) {
		return zero, errPortablePersonOrigin
	}
	return envelope.Claim, nil
}
