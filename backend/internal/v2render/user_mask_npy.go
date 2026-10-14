package v2render

import (
	"context"
	"errors"
	"io"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/v2input"
)

// VerifyUserMaskNPY preserves the Render validation contract while sharing the
// same bounded streaming verifier with semantic Apply. Caller closes reader.
func VerifyUserMaskNPY(ctx context.Context, reader io.Reader, expectedBytes int64, expectedSHA string, width, height int64) error {
	err := masknpy.VerifyFloat32MaskNPY(ctx, reader, expectedBytes, expectedSHA, width, height)
	if errors.Is(err, masknpy.ErrInvalid) {
		return v2input.ErrInvalid
	}
	return err
}
