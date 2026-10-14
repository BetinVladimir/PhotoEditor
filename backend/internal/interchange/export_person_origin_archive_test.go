package interchange

import (
	"archive/zip"
	"bytes"
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type originCurrentStub struct {
	calls int
	deny  bool
}

func (s *originCurrentStub) RecheckExportPersonOrigin(_ context.Context, _ PortablePersonOriginClaim) error {
	s.calls++
	if s.deny {
		return v2input.ErrInvalid
	}
	return nil
}

func TestExportPersonOriginArchiveRequiresPhysicalSignedReceipt(t *testing.T) {
	receipt, scope, keys := portablePersonOriginFixture(t)
	path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
	manifest := exportBundleManifest{PersonOrigins: []importBundlePersonOrigin{{
		EntryID: scope.EntryID, Recipe: scope.Recipe, Override: scope.Override,
		MaskRef: scope.MaskRef, MaskPixels: scope.MaskPixels, Path: path, SHA: rawSHA(receipt),
	}}, Inventory: []exportBundleInventoryEntry{{
		ID:   uuid.NewSHA1(scope.EntryID, []byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())),
		Path: path, SHA: rawSHA(receipt), Contract: "PersonOriginReceipt",
	}}}
	raw, err := canonical.Marshal(manifest)
	if err != nil {
		t.Fatal(err)
	}
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	member, err := writer.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = member.Write(receipt); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.NewReader(bytes.NewReader(buffer.Bytes()), int64(buffer.Len()))
	if err != nil {
		t.Fatal(err)
	}
	files := []VerifiedExportZIPFile{{Path: path, Bytes: int64(len(receipt)), SHA256: rawSHA(receipt)}}
	expected := map[string]PortablePersonOriginExpectation{path: scope}
	current := &originCurrentStub{}
	authority, err := NewExportPersonOriginArchiveAuthority(expected, keys, current)
	if err != nil {
		t.Fatal(err)
	}
	delete(expected, path)
	if len(authority.expected) != 1 {
		t.Fatal("caller mutated owned expectations")
	}
	expected[path] = scope
	if _, err := NewExportPersonOriginArchiveAuthority(expected, keys, nil); err == nil {
		t.Fatal("live origin authority omitted")
	}
	if err := VerifyExportPersonOriginArchive(context.Background(), archive, raw, files, expected, keys); err != nil {
		t.Fatalf("signed physical receipt refused: %v", err)
	}
	claims, err := verifyExportPersonOriginArchiveClaims(context.Background(), archive, raw, files,
		authority.expected, authority.trusted, authority.current)
	if err != nil || len(claims) != 1 || current.calls != 1 {
		t.Fatal("current producer was not rechecked", err)
	}
	current.deny = true
	if _, err := verifyExportPersonOriginArchiveClaims(context.Background(), archive, raw, files,
		authority.expected, authority.trusted, authority.current); err == nil {
		t.Fatal("stale producer accepted")
	}
	tampered := append([]VerifiedExportZIPFile(nil), files...)
	tampered[0].SHA256 = rawSHA([]byte("different"))
	if err := VerifyExportPersonOriginArchive(context.Background(), archive, raw, tampered, expected, keys); err == nil {
		t.Fatal("receipt detached from physical inventory")
	}
	wrongManifest := manifest
	wrongManifest.Inventory = append([]exportBundleInventoryEntry(nil), manifest.Inventory...)
	wrongManifest.Inventory[0].Contract = "Artifact"
	wrongRaw, err := canonical.Marshal(wrongManifest)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExportPersonOriginArchive(context.Background(), archive, wrongRaw, files, expected, keys); err == nil {
		t.Fatal("receipt without exact inventory contract accepted")
	}
	wrong := scope
	wrong.Source.AssetVersionID = scope.EntryID
	if err := VerifyExportPersonOriginArchive(context.Background(), archive, raw, files,
		map[string]PortablePersonOriginExpectation{path: wrong}, keys); err == nil {
		t.Fatal("foreign source accepted")
	}
}
