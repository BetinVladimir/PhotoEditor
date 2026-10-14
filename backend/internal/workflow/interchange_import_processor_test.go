package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"strings"
	"testing"
	"time"
)

type interchangeImportPorts struct {
	candidate         *InterchangeImportCandidate
	body              string
	opened, continued bool
	failure           error
}

func (p *interchangeImportPorts) NextImport(context.Context) (*InterchangeImportCandidate, error) {
	return p.candidate, nil
}
func (p *interchangeImportPorts) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	p.opened = true
	return io.NopCloser(strings.NewReader(p.body)), nil
}
func (p *interchangeImportPorts) ContinueInterchangeImport(_ context.Context, c InterchangeImportCandidate, raw []byte) error {
	p.continued = true
	if c.Workflow != p.candidate.Workflow || string(raw) != p.body {
		return errors.New("candidate substitution")
	}
	return p.failure
}
func TestInterchangeImportProcessorPinnedDispatchAndRefusal(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "size", "bucket", "snapshot", "cancelled", "idle", "continuation", "awaiting"} {
		t.Run(mode, func(t *testing.T) {
			body := `{"schema_version":1}`
			sum := sha256.Sum256([]byte(body))
			digest := hex.EncodeToString(sum[:])
			c := &InterchangeImportCandidate{Tenant: uuid.New(), Workflow: uuid.New(), Run: uuid.New(), Project: uuid.New(), Artifact: uuid.New(), Snapshot: digest, SHA: digest, Bytes: int64(len(body)), Bucket: "photos", Key: "sql-authorized-object", Version: "version"}
			ports := &interchangeImportPorts{candidate: c, body: body}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("continuation refused")
			switch mode {
			case "hash":
				c.SHA = strings.Repeat("a", 64)
			case "size":
				c.Bytes++
			case "bucket":
				c.Bucket = "other"
			case "snapshot":
				c.Snapshot = "invalid"
			case "cancelled":
				cancel()
			case "idle":
				ports.candidate = nil
			case "continuation":
				ports.failure = sentinel
			case "awaiting":
				ports.failure = ErrInterchangeImportAwaitingDecision
			}
			done, err := (InterchangeImportProcessor{Store: ports, Objects: ports, Continuation: ports, Bucket: "photos", Timeout: time.Minute}).ProcessOne(ctx)
			if mode == "valid" {
				if err != nil || !done || !ports.continued {
					t.Fatal("import not dispatched", err)
				}
				return
			}
			if mode == "awaiting" {
				if err != nil || !done || !ports.continued {
					t.Fatal("awaiting review was treated as failed", err)
				}
				return
			}
			if done {
				t.Fatal("invalid or refused import accepted")
			}
			if mode == "idle" {
				if err != nil || ports.opened {
					t.Fatal("idle performed IO", err)
				}
			} else if err == nil {
				t.Fatal("refusal lost")
			}
			if mode == "continuation" {
				if !errors.Is(err, sentinel) || !ports.continued {
					t.Fatal("original continuation error lost", err)
				}
			} else if ports.continued {
				t.Fatal("invalid object reached continuation")
			}
			if mode == "bucket" || mode == "snapshot" || mode == "cancelled" {
				if ports.opened {
					t.Fatal("denied object opened")
				}
			}
		})
	}
}

// A refused attempt must not poison the processor instance or prevent the next
// independently claimed workflow from being dispatched. SQL claim fairness is
// a separate integration requirement, not asserted by this fake store.
func TestInterchangeImportProcessorContinuesAfterRefusedCandidate(t *testing.T) {
	body := `{"schema_version":1}`
	sum := sha256.Sum256([]byte(body))
	digest := hex.EncodeToString(sum[:])
	candidate := func() *InterchangeImportCandidate {
		return &InterchangeImportCandidate{Tenant: uuid.New(), Workflow: uuid.New(), Run: uuid.New(), Project: uuid.New(), Artifact: uuid.New(), Snapshot: digest, SHA: digest, Bytes: int64(len(body)), Bucket: "photos", Key: "authorized", Version: "version"}
	}
	sentinel := errors.New("first workflow refused")
	ports := &interchangeImportPorts{candidate: candidate(), body: body, failure: sentinel}
	processor := InterchangeImportProcessor{Store: ports, Objects: ports, Continuation: ports, Bucket: "photos", Timeout: time.Minute}
	if done, err := processor.ProcessOne(context.Background()); done || !errors.Is(err, sentinel) {
		t.Fatal("refusal lost", done, err)
	}
	first := ports.candidate.Workflow
	ports.candidate = candidate()
	ports.failure = nil
	ports.continued = false
	if first == ports.candidate.Workflow {
		t.Fatal("fixture reused workflow")
	}
	if done, err := processor.ProcessOne(context.Background()); !done || err != nil || !ports.continued {
		t.Fatal("later candidate blocked", done, err)
	}
}
