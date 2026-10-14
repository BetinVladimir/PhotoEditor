package artifactjson

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
)

type repo struct {
	ticket          Reservation
	sha             string
	size            int64
	calls, complete int
	fail            bool
}

func (r *repo) Reserve(_ context.Context, _ Scope, sha string, size int64) (Reservation, error) {
	r.calls++
	if r.sha != "" && r.sha != sha {
		return Reservation{}, errors.New("content conflict")
	}
	r.sha = sha
	r.size = size
	return r.ticket, nil
}
func (r *repo) Complete(_ context.Context, _ Scope, t Reservation, o objectstore.Object) (Ref, error) {
	r.complete++
	if r.fail {
		return Ref{}, errors.New("database unavailable")
	}
	return Ref{t.ArtifactID, o.SHA256}, nil
}

type objects struct {
	put, recover int
	err          error
	bad          bool
	body         []byte
}

func (o *objects) PutImmutable(_ context.Context, key string, r io.Reader, size int64, sha, mime string) (objectstore.Object, error) {
	o.put++
	o.body, _ = io.ReadAll(r)
	return objectstore.Object{Key: key, VersionID: "v1", Bytes: size, SHA256: sha, ContentType: mime}, o.err
}
func (o *objects) RecoverImmutable(_ context.Context, key string, size int64, sha, mime string) (objectstore.Object, error) {
	o.recover++
	version := "v1"
	if o.bad {
		version = "null"
	}
	return objectstore.Object{Key: key, VersionID: version, Bytes: size, SHA256: sha, ContentType: mime}, nil
}
func setup() (Publisher, *repo, *objects, Scope) {
	r := &repo{ticket: Reservation{ArtifactID: uuid.New(), Token: uuid.New()}}
	o := &objects{}
	return Publisher{r, o}, r, o, Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "review_selection"}
}

func TestPublishCanonicalAndRecover(t *testing.T) {
	for _, err := range []error{nil, objectstore.ErrExists, objectstore.ErrUnavailable} {
		p, r, o, s := setup()
		o.err = err
		ref, e := p.Publish(context.Background(), s, []byte(` { "b":2, "a":1 } `))
		if e != nil {
			t.Fatal(e)
		}
		if string(o.body) != `{"a":1,"b":2}` || r.complete != 1 || ref.ID != r.ticket.ArtifactID {
			t.Fatal("bad canonical publish")
		}
		expected := 0
		if err != nil {
			expected = 1
		}
		if o.recover != expected {
			t.Fatal("wrong recovery policy")
		}
		r.ticket.Ready = &ref
		if _, e = p.Publish(context.Background(), s, []byte(`{"a":1,"b":2}`)); e != nil || o.put != 1 {
			t.Fatal("ready replay uploaded again", e)
		}
		if _, e = p.Publish(context.Background(), s, []byte(`{"a":2}`)); e == nil {
			t.Fatal("scope content changed")
		}
	}
}
func TestFailedRegistrationRetriesSameArtifact(t *testing.T) {
	p, r, o, s := setup()
	r.fail = true
	if _, err := p.Publish(context.Background(), s, []byte(`{}`)); err == nil {
		t.Fatal("failed registration accepted")
	}
	r.fail = false
	o.err = objectstore.ErrExists
	ref, err := p.Publish(context.Background(), s, []byte(`{}`))
	if err != nil || ref.ID != r.ticket.ArtifactID || o.recover != 1 {
		t.Fatal("could not recover prior object", err)
	}
}
func TestInvalidUploadNeverBecomesReady(t *testing.T) {
	p, r, o, s := setup()
	o.err = objectstore.ErrExists
	o.bad = true
	if _, err := p.Publish(context.Background(), s, []byte(`{}`)); !errors.Is(err, objectstore.ErrIntegrity) || r.complete != 0 {
		t.Fatal("invalid version registered", err)
	}
	o.bad = false
	o.err = objectstore.ErrIntegrity
	if _, err := p.Publish(context.Background(), s, []byte(`{}`)); !errors.Is(err, objectstore.ErrIntegrity) || o.recover != 1 || r.complete != 0 {
		t.Fatal("integrity failure retried", err)
	}
}
func TestInvalidDocumentHasNoReservation(t *testing.T) {
	p, r, _, s := setup()
	for _, raw := range []string{`null`, `[]`, `{"x":1,"x":2}`, `{"x":NaN}`, `{"x":9007199254740992}`} {
		if _, err := p.Publish(context.Background(), s, []byte(raw)); err == nil {
			t.Fatal("accepted", raw)
		}
	}
	if r.calls != 0 {
		t.Fatal("invalid document persisted")
	}
}

func TestV2TransportPurposeIsBoundedAndCanonical(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New()}
	for _, purpose := range []string{"v2_input_chunk_0", "v2_input_chunk_1953", "v2_input_manifest", "v2_session_plan_params"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err != nil {
			t.Fatalf("valid purpose %q rejected: %v", purpose, err)
		}
	}
	for _, purpose := range []string{"v2_input_chunk_01", "v2_input_chunk_1954", "v2_input_chunk_-1", "v2_input_manifest_0"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err == nil {
			t.Fatalf("invalid purpose %q accepted", purpose)
		}
	}
}

func TestV22AnalysisPurposesAndLimits(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New()}
	for _, purpose := range []string{"v2_analysis_input_snapshot", "v2_analysis_context", "v2_analysis_consents", "v2_analysis_describe_params_0", "v2_analysis_describe_params_39"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err != nil {
			t.Fatalf("valid purpose %q rejected: %v", purpose, err)
		}
	}
	for _, purpose := range []string{"v2_analysis_describe_params_00", "v2_analysis_describe_params_40", "v2_analysis_describe_params_-1"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err == nil {
			t.Fatalf("invalid purpose %q accepted", purpose)
		}
	}
	if maxDocumentBytes("v2_analysis_input_snapshot") != 8388608 ||
		maxDocumentBytes("v2_analysis_consents") != 8388608 ||
		maxDocumentBytes("v2_analysis_context") != 1048576 ||
		maxDocumentBytes("v2_analysis_describe_params_0") != 1048576 {
		t.Fatal("V22 document limits mismatch")
	}
}

func TestV23TrainingManifestPurposeAndLimit(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), Purpose: "v23_training_manifest"}
	if _, err := DocumentKey(s, uuid.New()); err != nil {
		t.Fatalf("TrainingManifest purpose rejected: %v", err)
	}
	if maxDocumentBytes(s.Purpose) != 8*1048576 {
		t.Fatal("TrainingManifest must fit the 5000-example contract")
	}
	s.Purpose = "v23_training_snapshot"
	if _, err := DocumentKey(s, uuid.New()); err != nil {
		t.Fatalf("ArtifactSnapshot purpose rejected: %v", err)
	}
	if maxDocumentBytes(s.Purpose) != 1048576 {
		t.Fatal("ArtifactSnapshot must remain bounded to one MiB")
	}
}

func TestLensPublicationPurposesAreExplicitAndBounded(t *testing.T) {
	s := Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New()}
	for _, purpose := range []string{"v25_lens_params", "v25_lens_original_binding"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err != nil || maxDocumentBytes(purpose) != 1048576 {
			t.Fatal(purpose, err)
		}
	}
	for _, purpose := range []string{"v25_lens_params_0", "v25_lens_original", "v25_lens_any"} {
		s.Purpose = purpose
		if _, err := DocumentKey(s, uuid.New()); err == nil {
			t.Fatal("unrecognized lens purpose accepted", purpose)
		}
	}
}

func TestAssemblyParamsPublishKeepsLegacyBoundAndRejectsWrongPurpose(t *testing.T) {
	p, r, o, s := setup()
	s.Purpose = "task_params"
	body := append([]byte(`{"value":"`), make([]byte, 1100000)...)
	for i := len(`{"value":"`); i < len(body); i++ {
		body[i] = 'a'
	}
	body = append(body, []byte(`"}`)...)
	if _, err := p.Publish(context.Background(), s, body); !errors.Is(err, objectstore.ErrInvalid) || r.calls != 0 || o.put != 0 {
		t.Fatal("legacy bound bypassed", err)
	}
	s.AssemblyLease = uuid.New()
	if _, err := p.Publish(context.Background(), s, body); err != nil || r.complete != 1 || o.put != 1 {
		t.Fatal("assembly publication failed", err)
	}
	s.Purpose = "review_selection"
	if _, err := p.Publish(context.Background(), s, []byte(`{}`)); !errors.Is(err, objectstore.ErrInvalid) || r.complete != 1 || o.put != 1 {
		t.Fatal("lease used outside assembly params", err)
	}
}
