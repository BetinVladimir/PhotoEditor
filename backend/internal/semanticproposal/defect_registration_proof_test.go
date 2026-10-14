package semanticproposal

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type defectProofFailCursor struct {
	closed bool
	next   int
	err    error
}

func (c *defectProofFailCursor) Next(context.Context) (v2input.Entry, bool, error) {
	c.next++
	return v2input.Entry{}, false, c.err
}
func (c *defectProofFailCursor) Close() error { c.closed = true; return nil }

func TestDefectRegistrationProofClosesFailedCursor(t *testing.T) {
	failure := errors.New("retained cursor failed")
	cursor := &defectProofFailCursor{err: failure}
	task := v2input.GraphTask{Built: v2input.Built{Count: 3}, Direct: make([]v2input.Ref, 3), Open: func(context.Context) (v2input.Cursor, error) { return cursor, nil }}
	proof, err := freezeDefectRegistrationProof(context.Background(), task)
	if !errors.Is(err, failure) || !cursor.closed || cursor.next != 1 || proof.Rows != nil {
		t.Fatal("failed cursor retained partial proof", err, cursor, proof)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cursor.closed = false
	cursor.next = 0
	if _, err := freezeDefectRegistrationProof(ctx, task); err != context.Canceled || cursor.next != 0 || cursor.closed {
		t.Fatal("cancelled freeze opened cursor", err, cursor)
	}
}

type defectProofEntriesCursor struct {
	entries []v2input.Entry
	index   int
	closed  bool
}

func (c *defectProofEntriesCursor) Next(context.Context) (v2input.Entry, bool, error) {
	if c.index == len(c.entries) {
		return v2input.Entry{}, false, nil
	}
	entry := c.entries[c.index]
	c.index++
	return entry, true, nil
}
func (c *defectProofEntriesCursor) Close() error { c.closed = true; return nil }
func TestDefectRegistrationProofRejectsChangedEntryDigest(t *testing.T) {
	entries := make([]v2input.Entry, 3)
	for i := range entries {
		entries[i] = v2input.Entry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}, Contract: "NpyArray"}
	}
	digest, err := canonical.Hash(entries)
	if err != nil {
		t.Fatal(err)
	}
	for changed := 0; changed < 3; changed++ {
		owned := append([]v2input.Entry(nil), entries...)
		if changed == 1 {
			owned[1].Ref.ArtifactID = uuid.New()
		}
		if changed == 2 {
			owned = append(owned, owned[0])
		}
		cursor := &defectProofEntriesCursor{entries: owned}
		task := v2input.GraphTask{Built: v2input.Built{Count: 3, SHA256: digest}, Direct: make([]v2input.Ref, 3), Open: func(context.Context) (v2input.Cursor, error) { return cursor, nil }}
		proof, err := freezeDefectRegistrationProof(context.Background(), task)
		if !cursor.closed {
			t.Fatal("cursor not closed")
		}
		if changed == 0 {
			if err != nil || proof.Count != 3 || proof.EntriesSHA != digest {
				t.Fatal("exact registration rejected", err)
			}
		} else if err != v2input.ErrInvalid || proof.Rows != nil {
			t.Fatal("changed registration retained", changed, err)
		}
	}
}
