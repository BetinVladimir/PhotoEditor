package semanticreview

import (
	"crypto/sha256"
	"encoding/hex"
	"photoeditor/backend/internal/review"
)

// The versioned profile is the exact bundled ICC recorded by profiles.lock.json.
// A new profile version must update this boundary and regression evidence.
const exposureICCProfileSHA = "a525b6d4afd90340bc8f69535dae83594925224f3569a3b90b988ac5f937e6e9"

// ExposureColorProfile is constructed from verified runtime ICC bytes, not an
// API-supplied checksum. It does not claim source decoding/scientific accuracy.
type ExposureColorProfile struct{ sha string }

func NewExposureColorProfile(raw []byte) (ExposureColorProfile, error) {
	if len(raw) != 648 {
		return ExposureColorProfile{}, ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != exposureICCProfileSHA {
		return ExposureColorProfile{}, ErrInvalid
	}
	return ExposureColorProfile{sha: exposureICCProfileSHA}, nil
}
func (profile ExposureColorProfile) valid() bool { return profile.sha == exposureICCProfileSHA }
func (profile ExposureColorProfile) VerifyWorkingImage(raw []byte) error {
	if !profile.valid() {
		return ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" || image.Color.ICCSHA256 != profile.sha {
		return ErrInvalid
	}
	return nil
}
