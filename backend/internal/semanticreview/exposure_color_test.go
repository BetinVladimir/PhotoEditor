package semanticreview

import (
	"encoding/json"
	"os"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestExposureColorProfileRequiresLockedBytesAndImageSHA(t *testing.T) {
	raw, err := os.ReadFile("../../../resources/icc/work_rec2020_gamma24_v1.icc")
	if err != nil {
		t.Fatal(err)
	}
	profile, err := NewExposureColorProfile(raw)
	if err != nil {
		t.Fatal(err)
	}
	changed := append([]byte(nil), raw...)
	changed[len(changed)-1] ^= 1
	for _, bad := range [][]byte{nil, raw[:len(raw)-1], append(append([]byte(nil), raw...), 0), changed} {
		if p, err := NewExposureColorProfile(bad); err == nil || p.valid() {
			t.Fatal("unlocked ICC accepted")
		}
	}
	lockRaw, err := os.ReadFile("../../../resources/icc/profiles.lock.json")
	if err != nil {
		t.Fatal(err)
	}
	var lock struct {
		Profiles []struct {
			ID    string `json:"id"`
			SHA   string `json:"sha256"`
			Bytes int    `json:"bytes"`
		}
	}
	if json.Unmarshal(lockRaw, &lock) != nil {
		t.Fatal("invalid ICC lock")
	}
	matches := 0
	for _, entry := range lock.Profiles {
		if entry.ID == "work_rec2020_gamma24_v1" {
			matches++
			if entry.SHA != profile.sha || entry.Bytes != len(raw) {
				t.Fatal("ICC boundary drift")
			}
		}
	}
	if matches != 1 {
		t.Fatal("ICC lock identity")
	}
	imageRaw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []map[string]any
	json.Unmarshal(imageRaw, &images)
	image := images[0]
	color := image["color"].(map[string]any)
	color["icc_sha256"] = profile.sha
	encoded, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	if err := profile.VerifyWorkingImage(encoded); err != nil {
		t.Fatal(err)
	}
	if err := (ExposureColorProfile{}).VerifyWorkingImage(encoded); err == nil {
		t.Fatal("missing runtime profile accepted")
	}
	for _, field := range []string{"icc_sha256", "profile_id", "encoding"} {
		original := color[field]
		color[field] = "invalid"
		encoded, _ := canonical.Marshal(image)
		if err := profile.VerifyWorkingImage(encoded); err == nil {
			t.Fatal("changed image color accepted", field)
		}
		color[field] = original
	}
}
