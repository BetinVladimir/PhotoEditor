package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"os"
	"path/filepath"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"strings"
	"testing"
)

func authoredFaceDeploymentFixture() AuthoredFaceDeployment {
	sha := strings.Repeat("a", 64)
	set := map[string]struct{}{sha: {}}
	makeOp := func(op, backend string, vram int64) AuthoredFaceOperationDeployment {
		return AuthoredFaceOperationDeployment{
			Expected: v2qualification.Expected{Operation: op, Version: "1", Backend: backend, ModelLockSHA: sha, RuntimeLockSHA: sha, DeviceProfileSHA: sha, DatasetSHA: sha, IssuerID: uuid.New(), RequiredChecks: []string{"physical"}, CalibrationSHAs: set, TestFixtureSHAs: set, EvidenceSHAs: set, LicenseCleared: true},
			Report:   v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}, RAM: 1 << 30, VRAM: vram, Scratch: 1 << 30, Output: 1 << 28}
	}
	return AuthoredFaceDeployment{SchemaVersion: 1, Tenants: []uuid.UUID{uuid.New()}, Detector: makeOp("face.detect", "mps", 1<<30), Landmarks: makeOp("face.landmarks", "mps", 1<<30), Parser: makeOp("face.parse", "mps", 1<<30), Skin: makeOp("mask.face_skin", "cpu", 0), Score: .5, MaxFaces: 16, PreviewPolicy: "declared_encoding_alpha8_icc_v2"}
}
func TestAuthoredFaceDeploymentPhysicalFileAndPin(t *testing.T) {
	d := authoredFaceDeploymentFixture()
	raw, err := canonical.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "deployment.json")
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	pin := hex.EncodeToString(sum[:])
	got, err := LoadAuthoredFaceDeployment(path, pin)
	if err != nil || got.Detector.Expected.ModelLockSHA != d.Detector.Expected.ModelLockSHA {
		t.Fatalf("valid pinned deployment refused: %v", err)
	}
	if _, err = LoadAuthoredFaceDeployment(path, strings.Repeat("b", 64)); err == nil {
		t.Fatal("wrong SHA accepted")
	}
	alias := path + ".symlink"
	if err = os.Symlink(path, alias); err != nil {
		t.Fatal(err)
	}
	if _, err = LoadAuthoredFaceDeployment(alias, pin); err == nil {
		t.Fatal("symlink accepted")
	}
	// Even a re-pinned noncanonical/duplicate member document is refused.
	for _, body := range [][]byte{append(append([]byte{}, raw...), '\n'), append([]byte(`{"schema_version":1,`), raw[1:]...)} {
		if err = os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
		sum = sha256.Sum256(body)
		if _, err = LoadAuthoredFaceDeployment(path, hex.EncodeToString(sum[:])); err == nil {
			t.Fatal("noncanonical deployment accepted")
		}
	}
}
func TestAuthoredFaceDeploymentClosedAuthority(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*AuthoredFaceDeployment)
	}{
		{"tenant duplicate", func(d *AuthoredFaceDeployment) { d.Tenants = append(d.Tenants, d.Tenants[0]) }},
		{"CPU VRAM", func(d *AuthoredFaceDeployment) { d.Skin.VRAM = 1 }},
		{"CPU model relabel", func(d *AuthoredFaceDeployment) { d.Parser.Expected.Backend = "cpu" }},
		{"foreign runtime", func(d *AuthoredFaceDeployment) { d.Parser.Expected.RuntimeLockSHA = strings.Repeat("b", 64) }},
		{"missing report", func(d *AuthoredFaceDeployment) { d.Detector.Report.ArtifactID = uuid.Nil }},
		{"uncleared", func(d *AuthoredFaceDeployment) { d.Skin.Expected.LicenseCleared = false }},
		{"empty checks", func(d *AuthoredFaceDeployment) { d.Parser.Expected.RequiredChecks = nil }},
		{"noncanonical pin", func(d *AuthoredFaceDeployment) { d.Detector.Expected.ModelLockSHA = strings.Repeat("A", 64) }},
		{"unbounded memory", func(d *AuthoredFaceDeployment) { d.Skin.RAM = 9007199254740992 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := authoredFaceDeploymentFixture()
			tc.mutate(&d)
			if d.validate() == nil {
				t.Fatal("invalid deployment accepted")
			}
		})
	}
}
