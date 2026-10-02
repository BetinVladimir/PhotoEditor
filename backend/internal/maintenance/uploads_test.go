package maintenance

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type uploadCleanupPorts struct {
	ticket                        *UploadAbortCleanup
	expired, aborted, finished    bool
	expireErr, claimErr, abortErr error
	objectKey, multipartID        string
}

func (p *uploadCleanupPorts) ExpireOne(context.Context) (bool, error) { return p.expired, p.expireErr }
func (p *uploadCleanupPorts) Claim(context.Context) (*UploadAbortCleanup, error) {
	return p.ticket, p.claimErr
}
func (p *uploadCleanupPorts) Finish(context.Context, UploadAbortCleanup) (bool, error) {
	p.finished = true
	return true, nil
}
func (p *uploadCleanupPorts) AbortMultipart(_ context.Context, key, multipart string) error {
	p.aborted, p.objectKey, p.multipartID = true, key, multipart
	return p.abortErr
}

func uploadCleanupFixture() *UploadAbortCleanup {
	return &UploadAbortCleanup{ID: uuid.New(), Token: uuid.New(), UploadID: uuid.New(), TenantID: uuid.New(), ObjectKey: "staging/tenants/t/uploads/u", MultipartID: "multipart"}
}

func TestUploadCleanerExpiresAndConfirmsMultipartAbort(t *testing.T) {
	p := &uploadCleanupPorts{expired: true, ticket: uploadCleanupFixture()}
	changed, err := (UploadCleaner{Repository: p, Objects: p}).CleanOne(context.Background())
	if err != nil || !changed || !p.aborted || !p.finished || p.objectKey != p.ticket.ObjectKey || p.multipartID != p.ticket.MultipartID {
		t.Fatal("upload cleanup not completed", changed, err)
	}
}

func TestUploadCleanerTreatsMissingMultipartAsClean(t *testing.T) {
	p := &uploadCleanupPorts{ticket: uploadCleanupFixture(), abortErr: objectstore.ErrMissing}
	changed, err := (UploadCleaner{Repository: p, Objects: p}).CleanOne(context.Background())
	if err != nil || !changed || !p.finished {
		t.Fatal("missing multipart was not settled", changed, err)
	}
}

func TestUploadCleanerDoesNotFinishUnknownAbort(t *testing.T) {
	p := &uploadCleanupPorts{ticket: uploadCleanupFixture(), abortErr: errors.New("storage unavailable")}
	changed, err := (UploadCleaner{Repository: p, Objects: p}).CleanOne(context.Background())
	if err == nil || changed || p.finished {
		t.Fatal("failed abort crossed confirmation boundary", changed, err)
	}
}
