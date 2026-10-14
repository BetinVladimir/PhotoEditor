package upload

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type completionLeaseFake struct {
	lease                         *CompletionLease
	renew                         bool
	claimErr, renewErr, finishErr error
	renews, releases, finishes    int
	loseOnRenew                   int
	payload, config               json.RawMessage
	fails                         int
}

func (f *completionLeaseFake) Claim(context.Context) (*CompletionLease, error) {
	return f.lease, f.claimErr
}
func (f *completionLeaseFake) Renew(context.Context, CompletionLease) (bool, error) {
	f.renews++
	if f.loseOnRenew > 0 && f.renews == f.loseOnRenew {
		return false, nil
	}
	return f.renew, f.renewErr
}
func (f *completionLeaseFake) Release(context.Context, CompletionLease) (bool, error) {
	f.releases++
	return true, nil
}
func (f *completionLeaseFake) Finish(_ context.Context, _ CompletionLease, _ objectstore.Object, payload json.RawMessage, _ string, config json.RawMessage, _ string) (bool, error) {
	f.finishes++
	f.payload, f.config = payload, config
	return f.finishErr == nil, f.finishErr
}
func (f *completionLeaseFake) Fail(_ context.Context, _ CompletionLease, _ objectstore.Object, payload json.RawMessage, _ string, config json.RawMessage, _ string) (bool, error) {
	f.fails++
	f.payload, f.config = payload, config
	return f.finishErr == nil, f.finishErr
}

type completionObjectFake struct {
	completeErr, verifyErr        error
	completes, recovers, verifies int
	pending                       objectstore.PendingObject
	object                        objectstore.Object
}

func (f *completionObjectFake) CompleteMultipart(context.Context, string, string, string, int64, []objectstore.UploadedPart) (objectstore.PendingObject, error) {
	f.completes++
	return f.pending, f.completeErr
}
func (f *completionObjectFake) RecoverCompletedMultipart(context.Context, string, string, int64) (objectstore.PendingObject, error) {
	f.recovers++
	return f.pending, nil
}
func (f *completionObjectFake) VerifyPending(context.Context, objectstore.PendingObject, string) (objectstore.Object, error) {
	f.verifies++
	return f.object, f.verifyErr
}

func TestCompletionProcessorSettlesExpectedHashMismatch(t *testing.T) {
	lease, pending, object := completionFixture()
	expected := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	lease.ExpectedSHA = &expected
	leasing := &completionLeaseFake{lease: &lease, renew: true}
	objects := &completionObjectFake{pending: pending, object: object, verifyErr: objectstore.ErrIntegrity}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects, HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond, WorkTimeout: time.Second}).ProcessOne(context.Background())
	if err != nil || !changed || leasing.fails != 1 || leasing.finishes != 0 || leasing.releases != 0 {
		t.Fatal("hash mismatch not settled", changed, err, leasing.fails, leasing.finishes, leasing.releases)
	}
}

func completionFixture() (CompletionLease, objectstore.PendingObject, objectstore.Object) {
	lease := CompletionLease{
		UploadID: uuid.New(), Token: uuid.New(), TenantID: uuid.New(), ArtifactID: uuid.New(), SnapshotID: uuid.New(), RunID: uuid.New(),
		ObjectKey: "staging/tenants/test/uploads/test", MultipartID: "multipart", MediaType: "image/jpeg", Filename: "photo.jpg",
		ExpectedBytes: 4, Parts: []objectstore.UploadedPart{{PartNumber: 1, ETag: "etag"}},
	}
	pending := objectstore.PendingObject{Key: lease.ObjectKey, VersionID: "v1", ContentType: lease.MediaType, Bytes: lease.ExpectedBytes}
	object := objectstore.Object{Key: lease.ObjectKey, VersionID: "v1", ContentType: lease.MediaType, Bytes: lease.ExpectedBytes, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"}
	return lease, pending, object
}

func TestCompletionProcessorCommitsCanonicalIngestDocuments(t *testing.T) {
	lease, pending, object := completionFixture()
	leasing := &completionLeaseFake{lease: &lease, renew: true}
	objects := &completionObjectFake{pending: pending, object: object}
	processor := CompletionProcessor{Leases: leasing, Objects: objects, HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond, WorkTimeout: time.Second}
	changed, err := processor.ProcessOne(context.Background())
	if err != nil || !changed || objects.completes != 1 || objects.verifies != 1 || leasing.finishes != 1 || leasing.releases != 0 {
		t.Fatal("completion failed", changed, err, objects, leasing)
	}
	var payload map[string]any
	if json.Unmarshal(leasing.payload, &payload) != nil || payload["suffix"] != ".jpg" || payload["tile"] != float64(512) || payload["analyze"] != true {
		t.Fatal("invalid ingest payload", string(leasing.payload))
	}
	var config map[string]any
	if json.Unmarshal(leasing.config, &config) != nil || config["source"] != "upload" {
		t.Fatal("invalid run config", string(leasing.config))
	}
}

func TestCompletionProcessorRecoversLostCompleteResponse(t *testing.T) {
	lease, pending, object := completionFixture()
	leasing := &completionLeaseFake{lease: &lease, renew: true}
	objects := &completionObjectFake{pending: pending, object: object, completeErr: objectstore.ErrUnavailable}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects, HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond, WorkTimeout: time.Second}).ProcessOne(context.Background())
	if err != nil || !changed || objects.recovers != 1 || leasing.finishes != 1 {
		t.Fatal("lost completion not recovered", changed, err, objects.recovers, leasing.finishes)
	}
}

func TestCompletionProcessorStopsBeforeStorageAfterLeaseLoss(t *testing.T) {
	lease, pending, object := completionFixture()
	leasing := &completionLeaseFake{lease: &lease, renew: false}
	objects := &completionObjectFake{pending: pending, object: object}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects, HeartbeatInterval: 10 * time.Millisecond, RenewTimeout: 10 * time.Millisecond, WorkTimeout: time.Second}).ProcessOne(context.Background())
	if changed || !errors.Is(err, ErrCompletionLeaseLost) || objects.completes != 0 || leasing.finishes != 0 || leasing.releases != 1 {
		t.Fatal("lease loss crossed side-effect boundary", changed, err, objects.completes, leasing.finishes, leasing.releases)
	}
}

func TestCompletionProcessorDoesNotCommitAfterFinalLeaseLoss(t *testing.T) {
	lease, pending, object := completionFixture()
	leasing := &completionLeaseFake{lease: &lease, renew: true, loseOnRenew: 2}
	objects := &completionObjectFake{pending: pending, object: object}
	changed, err := (CompletionProcessor{Leases: leasing, Objects: objects,
		HeartbeatInterval: time.Second, RenewTimeout: time.Second, WorkTimeout: time.Minute}).ProcessOne(context.Background())
	if changed || !errors.Is(err, ErrCompletionLeaseLost) || objects.verifies != 1 || leasing.finishes != 0 || leasing.fails != 0 || leasing.releases != 1 {
		t.Fatal("final lease loss reached commit", changed, err, leasing, objects)
	}
}

func TestDecodeCompletionPartsRejectsTrailingOrMalformedJSON(t *testing.T) {
	for _, raw := range []string{
		`[{"part_no":1,"etag":"etag"}] {}`,
		`[{"part_no":1,"etag":"etag","unknown":true}]`,
		`null`,
		`[]`,
		`[{"part_no":1,"etag":"bad\u0000etag"}]`,
	} {
		var parts []objectstore.UploadedPart
		if err := decodeCompletionParts([]byte(raw), &parts); err == nil {
			t.Fatal("invalid persisted parts accepted", raw)
		}
	}
}
