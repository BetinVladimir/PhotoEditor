package semanticproposal

import (
	"photoeditor/backend/internal/colorprofile"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// TransferColorProfile owns a work-profile SHA obtained by verifying deployed
// registry files. The registry directory is server configuration, not API input.
type TransferColorProfile struct{ sha string }

func LoadTransferColorProfile(directory string) (TransferColorProfile, error) {
	sha, err := colorprofile.LoadWorkSHA(directory)
	if err != nil {
		return TransferColorProfile{}, err
	}
	return TransferColorProfile{sha: sha}, nil
}

func (p TransferColorProfile) VerifyWorkingImage(raw []byte) error {
	if p.sha == "" {
		return v2input.ErrInvalid
	}
	image, err := review.ParseWorkingImage(raw)
	if err != nil || image.DType != "float32" || image.Color.ProfileID != "work_rec2020_gamma24_v1" || image.Color.Encoding != "gamma24" || image.Color.ICCSHA256 != p.sha {
		return v2input.ErrInvalid
	}
	return nil
}
