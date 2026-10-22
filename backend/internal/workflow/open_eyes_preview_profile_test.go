package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"photoeditor/backend/internal/media"
)

// The before-side SQL gate in 01952 computes this exact canonical media
// profile. A drift here would leave valid donor reviews permanently pending.
func TestOpenEyesBeforePreviewSQLProfileMatchesMediaCompiler(t *testing.T) {
	for _, srgb := range []string{strings.Repeat("a", 64), strings.Repeat("0", 64)} {
		profile, err := media.ProfileHash("preview1600", srgb)
		if err != nil {
			t.Fatal(err)
		}
		wire := fmt.Sprintf(`{"icc_sha256":"%s","long_edge":1600,"metadata_policy":"stripped","no_upscale":true,"quality":88,"renderer_policy":"declared_encoding_alpha8_icc_v2","schema_version":1,"variant":"preview1600"}`, srgb)
		digest := sha256.Sum256([]byte(wire))
		if profile != hex.EncodeToString(digest[:]) {
			t.Fatal("donor before-preview SQL profile drift")
		}
	}
}
