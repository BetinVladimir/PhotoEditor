package interchange

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type personOriginRecordObjects map[string][]byte

func (o personOriginRecordObjects) Open(_ context.Context, object objectstore.Object) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(o[object.Key])), nil
}

type personOriginRecordSource struct {
	object objectstore.Object
	calls  int
	change bool
}

func (s *personOriginRecordSource) LookupPersonOriginReceipt(_ context.Context, _ []byte, _ v2input.Ref) (objectstore.Object, error) {
	s.calls++
	value := s.object
	if s.change && s.calls > 1 {
		value.VersionID = "changed"
	}
	return value, nil
}

type personOriginRecordWriter struct {
	calls        int
	parsed, body []byte
	ref          v2input.Ref
}

type personOriginRecordScope struct {
	calls    int
	deny     bool
	expected PortablePersonOriginExpectation
}

func (s *personOriginRecordScope) DiscoverEntry(_ context.Context, _ []byte, entry uuid.UUID, model string) ([]PortablePersonOriginExpectation, error) {
	if s.expected.EntryID != entry || s.expected.ModelLockSHA != model {
		return nil, v2input.ErrInvalid
	}
	return []PortablePersonOriginExpectation{s.expected}, nil
}

func (s *personOriginRecordScope) QualifyImportedPersonOriginScope(_ context.Context, _ []byte, _ PortablePersonOriginExpectation) error {
	s.calls++
	if s.deny {
		return v2input.ErrInvalid
	}
	return nil
}

func (w *personOriginRecordWriter) RecordPersonOriginReceipt(_ context.Context, parsed []byte, ref v2input.Ref, body []byte) error {
	w.calls++
	w.parsed = append([]byte(nil), parsed...)
	w.body = append([]byte(nil), body...)
	w.ref = ref
	return nil
}

func TestPersonOriginRecorderVerifiesPhysicalReceiptBeforeSQL(t *testing.T) {
	for _, mode := range []string{"valid", "tampered", "changed_object", "no_trust", "scope"} {
		t.Run(mode, func(t *testing.T) {
			parsed, raw, expected, keys, staged := importedPersonOriginSelectionFixture(t)
			parsedRef := v2input.Ref{ArtifactID: expected.EntryID, SHA256: rawSHA(parsed)}
			parsedObject := objectstore.Object{Key: "parsed", VersionID: "v1", SHA256: parsedRef.SHA256, Bytes: int64(len(parsed)), ContentType: "application/json"}
			parsedSource := &reviewParsedSourceStub{ref: parsedRef, object: parsedObject}
			receiptObject := objectstore.Object{Key: "receipt", VersionID: "v1", SHA256: staged.SHA256, Bytes: int64(len(raw)), ContentType: "application/octet-stream"}
			receiptSource := &personOriginRecordSource{object: receiptObject, change: mode == "changed_object"}
			writer := &personOriginRecordWriter{}
			scope := &personOriginRecordScope{deny: mode == "scope"}
			body := append([]byte(nil), raw...)
			if mode == "tampered" {
				body[0] = '!'
			}
			if mode == "no_trust" {
				keys = nil
			}
			service := ImportPersonOriginRecordService{Parsed: parsedSource, Source: receiptSource,
				Objects: personOriginRecordObjects{"parsed": parsed, "receipt": body}, Writer: writer, Trusted: keys, Scope: scope}
			result, err := service.Record(context.Background(), expected)
			if mode == "valid" {
				if err != nil || writer.calls != 1 || writer.ref != staged || result.ReceiptFile != staged ||
					!bytes.Equal(writer.body, raw) || !bytes.Equal(writer.parsed, parsed) || receiptSource.calls != 2 || scope.calls != 2 {
					t.Fatal("qualified receipt not recorded", err)
				}
			} else if err == nil || writer.calls != 0 {
				t.Fatal("unqualified receipt reached SQL", mode)
			}
		})
	}
}

func TestPersonOriginRecorderDiscoversEntryBeforeWriting(t *testing.T) {
	parsed, raw, expected, keys, staged := importedPersonOriginSelectionFixture(t)
	parsedRef := v2input.Ref{ArtifactID: expected.EntryID, SHA256: rawSHA(parsed)}
	parsedSource := &reviewParsedSourceStub{ref: parsedRef, object: objectstore.Object{
		Key: "parsed", VersionID: "v1", SHA256: parsedRef.SHA256,
		Bytes: int64(len(parsed)), ContentType: "application/json"}}
	receiptSource := &personOriginRecordSource{object: objectstore.Object{
		Key: "receipt", VersionID: "v1", SHA256: staged.SHA256,
		Bytes: int64(len(raw)), ContentType: "application/octet-stream"}}
	writer := &personOriginRecordWriter{}
	scope := &personOriginRecordScope{expected: expected}
	service := ImportPersonOriginRecordService{Parsed: parsedSource, Source: receiptSource,
		Objects: personOriginRecordObjects{"parsed": parsed, "receipt": raw}, Writer: writer,
		Trusted: keys, Scope: scope}
	verified, err := service.RecordEntry(context.Background(), expected.EntryID, expected.ModelLockSHA)
	if err != nil || len(verified) != 1 || verified[0].ReceiptFile != staged || writer.calls != 1 {
		t.Fatal("discovered entry not recorded", err)
	}
	if _, err := service.RecordEntry(context.Background(), uuid.New(), expected.ModelLockSHA); err == nil {
		t.Fatal("foreign entry reached writer")
	}
}
