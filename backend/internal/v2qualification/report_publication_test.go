package v2qualification

// Synthetic unit bytes test publication mechanics, never measured qualification.
import (
	"bytes"
	"context"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

type publicationUnitRepo struct {
	ticket   ReportPublicationReservation
	calls    []string
	deny     int
	checks   int
	complete int
	scope    ReportPublicationScope
}

func (r *publicationUnitRepo) Reserve(_ context.Context, s ReportPublicationScope, _ Expected, _ string, _ int64) (ReportPublicationReservation, error) {
	r.calls = append(r.calls, "reserve")
	r.scope = s
	return r.ticket, nil
}
func (r *publicationUnitRepo) Check(context.Context, ReportPublicationScope, Expected, ReportPublicationReservation, string, int64) error {
	r.checks++
	if r.checks == r.deny {
		return ErrUnqualified
	}
	return nil
}
func (r *publicationUnitRepo) Start(context.Context, ReportPublicationScope, ReportPublicationReservation) error {
	r.calls = append(r.calls, "start")
	return nil
}
func (r *publicationUnitRepo) Close(context.Context, ReportPublicationScope, ReportPublicationReservation, objectstore.Object) error {
	r.calls = append(r.calls, "close")
	return nil
}
func (r *publicationUnitRepo) Complete(_ context.Context, _ ReportPublicationScope, _ Expected, t ReportPublicationReservation, o objectstore.Object) (artifactjson.Ref, error) {
	r.complete++
	return artifactjson.Ref{ID: t.Artifact, SHA256: o.SHA256}, nil
}

type publicationUnitObjects struct {
	body                      []byte
	object                    objectstore.Object
	put, recover, open        int
	putErr, openErr, closeErr error
	nilReader                 bool
	closed                    int
	corrupt                   bool
}
type publicationUnitReader struct {
	io.Reader
	owner *publicationUnitObjects
}

func (r publicationUnitReader) Close() error { r.owner.closed++; return r.owner.closeErr }
func (o *publicationUnitObjects) PutImmutable(_ context.Context, key string, reader io.Reader, size int64, sha, mime string) (objectstore.Object, error) {
	o.put++
	b, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Object{}, err
	}
	o.body = b
	o.object = objectstore.Object{Key: key, VersionID: "unit-version", SHA256: sha, Bytes: size, ContentType: mime}
	return o.object, o.putErr
}
func (o *publicationUnitObjects) RecoverImmutable(context.Context, string, int64, string, string) (objectstore.Object, error) {
	o.recover++
	return o.object, nil
}
func (o *publicationUnitObjects) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	o.open++
	if o.nilReader {
		return nil, o.openErr
	}
	b := o.body
	if o.corrupt {
		b = append(append([]byte(nil), b...), 1)
	}
	return publicationUnitReader{Reader: bytes.NewReader(b), owner: o}, o.openErr
}
func publicationUnitFixture(t *testing.T) ([]byte, Expected, ReportPublicationScope, *publicationUnitRepo, *publicationUnitObjects) {
	t.Helper()
	sha := func(c string) string { return strings.Repeat(c, 64) }
	issuer := uuid.New()
	report := Report{SchemaVersion: 1, Operation: "identity.compare", Version: "1", ModelLockSHA: sha("a"), RuntimeLockSHA: sha("b"), DeviceProfileSHA: sha("c"), DatasetSHA: sha("d"), InputFixtureSHAs: []string{sha("e")}, Checks: []Check{{ID: "quality", Passed: true, EvidenceSHA: sha("f")}}, Metrics: Metrics{P50MS: 1, P95MS: 2}, Status: "passed", IssuerID: issuer, CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	body, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	expected := Expected{Operation: report.Operation, Version: report.Version, Backend: "cpu", ModelLockSHA: report.ModelLockSHA, RuntimeLockSHA: report.RuntimeLockSHA, DeviceProfileSHA: report.DeviceProfileSHA, DatasetSHA: report.DatasetSHA, IssuerID: issuer, RequiredChecks: []string{"quality"}, TestFixtureSHAs: map[string]struct{}{sha("e"): {}}, EvidenceSHAs: map[string]struct{}{sha("f"): {}}, LicenseCleared: true}
	scope := ReportPublicationScope{Tenant: uuid.New(), Project: uuid.New(), Operator: uuid.New(), Key: "unit-only"}
	repo := &publicationUnitRepo{ticket: ReportPublicationReservation{Artifact: uuid.New(), Token: uuid.New(), WriterState: "idle"}}
	return body, expected, scope, repo, &publicationUnitObjects{}
}
func TestReportPublicationBytesAndErrorPaths(t *testing.T) {
	for _, mode := range []string{"normal", "bad-report", "deny-before-put", "deny-before-read", "deny-after-read", "open-error-reader", "nil-reader", "corrupt-read", "close-error", "exists-recovery"} {
		t.Run(mode, func(t *testing.T) {
			body, e, s, r, o := publicationUnitFixture(t)
			original := errors.New("original open error")
			switch mode {
			case "bad-report":
				body = []byte(`{}`)
			case "deny-before-put":
				r.deny = 1
			case "deny-before-read":
				r.deny = 2
			case "deny-after-read":
				r.deny = 3
			case "open-error-reader":
				o.openErr = original
				o.closeErr = errors.New("secondary close failure")
			case "nil-reader":
				o.nilReader = true
			case "corrupt-read":
				o.corrupt = true
			case "close-error":
				o.closeErr = errors.New("close failure")
			case "exists-recovery":
				o.putErr = objectstore.ErrExists
			}
			_, err := (ReportPublisher{Repository: r, Objects: o}).Publish(context.Background(), s, body, e)
			positive := mode == "normal" || mode == "exists-recovery"
			if positive {
				if err != nil || r.complete != 1 || o.closed != 1 || !bytes.Equal(body, o.body) {
					t.Fatal("valid publication changed", err)
				}
			} else {
				if err == nil || r.complete != 0 {
					t.Fatal("invalid publication completed")
				}
			}
			if mode == "bad-report" && (len(r.calls) != 0 || o.put != 0) {
				t.Fatal("invalid report reached authority/IO")
			}
			if mode == "deny-before-put" && o.put != 0 {
				t.Fatal("revoked producer wrote")
			}
			if mode == "open-error-reader" && (err != original || o.closed != 1) {
				t.Fatal("original error masked or stream leaked")
			}
		})
	}
}
func TestReportPublicationReadyReplayPinsVersion(t *testing.T) {
	body, e, s, r, o := publicationUnitFixture(t)
	p := ReportPublisher{Repository: r, Objects: o}
	if _, err := p.Publish(context.Background(), s, body, e); err != nil {
		t.Fatal(err)
	}
	ticket := o.object
	r.ticket.Ready = &ticket
	r.ticket.Pinned = &ticket
	r.ticket.WriterState = "closed"
	r.complete = 0
	o.put = 0
	o.recover = 0
	if _, err := p.Publish(context.Background(), s, body, e); err != nil {
		t.Fatal(err)
	}
	if o.put != 0 || o.recover != 0 || r.complete != 0 {
		t.Fatal("ready replay mutated storage")
	}
	ticket.VersionID = "null"
	r.ticket.Ready = &ticket
	if _, err := p.Publish(context.Background(), s, body, e); err == nil {
		t.Fatal("unversioned replay accepted")
	}
}
func TestReportPublicationClosedPendingDoesNotRecoverLatest(t *testing.T) {
	body, e, s, r, o := publicationUnitFixture(t)
	p := ReportPublisher{Repository: r, Objects: o}
	if _, err := p.Publish(context.Background(), s, body, e); err != nil {
		t.Fatal(err)
	}
	ticket := o.object
	r.ticket.Pinned = &ticket
	r.ticket.WriterState = "closed"
	o.put = 0
	o.recover = 0
	if _, err := p.Publish(context.Background(), s, body, e); err != nil {
		t.Fatal(err)
	}
	if o.put != 0 || o.recover != 0 {
		t.Fatal("closed pending used latest key")
	}
}
