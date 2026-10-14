package upload

import (
	"context"
	"testing"
	"time"

	"photoeditor/backend/internal/rawdevelopment"
)

func TestCompletionSourceRetainsSupportedFormatsAndRejectsMismatches(t *testing.T) {
	lease, _, _ := completionFixture()
	if !validCompletionSource(lease) {
		t.Fatal("valid JPEG completion source rejected")
	}
	for suffix, media := range formats {
		lease.Filename, lease.MediaType = "photo."+suffix, media
		if !validCompletionSource(lease) {
			t.Fatal("supported upload format rejected", suffix)
		}
	}
	lease.Filename, lease.MediaType = "photo.jpg", "image/png"
	if validCompletionSource(lease) {
		t.Fatal("filename and MIME mismatch accepted")
	}
	lease.Filename, lease.MediaType = "photo.exe", "image/jpeg"
	if validCompletionSource(lease) {
		t.Fatal("unsupported source suffix accepted")
	}
	lease.Filename, lease.MediaType = "photo.nef", formats["nef"]
	invalidProfile := "foreign_profile"
	lease.AssumeProfile = &invalidProfile
	if validCompletionSource(lease) {
		t.Fatal("foreign color profile accepted")
	}
	lease.AssumeProfile = nil
	lease.RAWDevelopment = &rawdevelopment.Policy{SchemaVersion: 1, Demosaic: "ahd",
		WBSource: "camera", HighlightPolicy: "clip",
		DecoderVersion: "rawpy-0.20.0/libraw-0.21.0"}
	if !validCompletionSource(lease) {
		t.Fatal("supported RAW policy rejected")
	}
	lease.Filename, lease.MediaType = "photo.png", formats["png"]
	if validCompletionSource(lease) {
		t.Fatal("RAW policy attached to RGB upload")
	}
	lease.RAWDevelopment = nil
	badSHA := "NOT-A-SHA"
	lease.ExpectedSHA = &badSHA
	if validCompletionSource(lease) {
		t.Fatal("invalid expected hash accepted before object IO")
	}
}

func TestCompletionSourceMismatchStopsBeforeObjectIO(t *testing.T) {
	lease, pending, object := completionFixture()
	lease.MediaType = "image/png"
	leasing := &completionLeaseFake{lease: &lease, renew: true}
	objects := &completionObjectFake{pending: pending, object: object}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects,
		HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond,
		WorkTimeout: time.Second}).ProcessOne(context.Background())
	if changed || err == nil || objects.completes != 0 || objects.verifies != 0 || leasing.releases != 1 {
		t.Fatal("mismatched source reached object IO", changed, err)
	}
}
