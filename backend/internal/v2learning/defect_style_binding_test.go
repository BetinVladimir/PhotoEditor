package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestReviewedDefectStyleBindsEffectiveRepair(t *testing.T) {
	image := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	defects := map[string]any{"enabled": true, "algorithm": "defectrepair_telea_v1",
		"policy": "review", "strength": .7,
		"diameter_face_fraction":   []float64{.002, .012},
		"preserve_permanent_marks": true}
	makeDocs := func(policy string, resolvedImage v2input.Ref,
		strength float64) ([]byte, v2input.Ref, []byte, v2input.Ref) {
		module := make(map[string]any, len(defects))
		for key, value := range defects {
			module[key] = value
		}
		module["policy"] = policy
		style := map[string]any{"schema_version": "2.0.0",
			"renderer_version": "photo_renderer_v2", "strength": .9,
			"defects": module}
		styleRaw, err := canonical.Marshal(style)
		if err != nil {
			t.Fatal(err)
		}
		styleDigest := sha256.Sum256(styleRaw)
		styleRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(styleDigest[:])}
		resolved := map[string]any{"mode": "fixed", "strength": strength,
			"params":  map[string]any{"style": styleRef, "image": resolvedImage},
			"modules": map[string]any{"defects": module}}
		resolvedHash, err := canonical.Hash(resolved)
		if err != nil {
			t.Fatal(err)
		}
		resolved["hash"] = resolvedHash
		resolvedRaw, err := canonical.Marshal(resolved)
		if err != nil {
			t.Fatal(err)
		}
		resolvedDigest := sha256.Sum256(resolvedRaw)
		resolvedRef := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(resolvedDigest[:])}
		return styleRaw, styleRef, resolvedRaw, resolvedRef
	}
	styleRaw, styleRef, resolvedRaw, resolvedRef := makeDocs("review", image, .8)
	if err := ValidateReviewedDefectStyle(styleRaw, resolvedRaw, styleRef, resolvedRef, image); err != nil {
		t.Fatal("valid reviewed style rejected", err)
	}
	wrongImage := image
	wrongImage.ArtifactID = uuid.New()
	for _, caseData := range []struct {
		policy   string
		image    v2input.Ref
		strength float64
	}{
		{"conservative_auto", image, .8},
		{"review", wrongImage, .8},
		{"review", image, 0},
	} {
		s, sr, r, rr := makeDocs(caseData.policy, caseData.image, caseData.strength)
		if ValidateReviewedDefectStyle(s, r, sr, rr, image) == nil {
			t.Fatal("unbound reviewed repair admitted", caseData)
		}
	}
	badRef := resolvedRef
	badRef.SHA256 = strings.Repeat("0", 64)
	if ValidateReviewedDefectStyle(styleRaw, resolvedRaw, styleRef, badRef, image) == nil {
		t.Fatal("changed resolved style hash admitted")
	}
}
