package upload

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func baseSession() Session {
	mid := "mp-id"
	col := uuid.New()
	return Session{
		ID:          uuid.New(),
		ProjectID:   uuid.New(),
		CollectionID: &col,
		Revision:    "1",
		ExpiresAt:   time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond),
		objectKey:   "tenant/upload/key",
		multipartID: &mid,
	}
}

func TestSameSignedSession_equal(t *testing.T) {
	s := baseSession()
	if !sameSignedSession(s, s) {
		t.Fatal("identical session must match")
	}
}

func TestSameSignedSession_nilMultipartID(t *testing.T) {
	a := baseSession()
	b := baseSession()
	b.ID = a.ID
	b.multipartID = nil
	if sameSignedSession(a, b) {
		t.Fatal("nil multipartID must not match")
	}
}

func TestSameSignedSession_nilCollectionID(t *testing.T) {
	a := baseSession()
	b := a
	b.CollectionID = nil
	if sameSignedSession(a, b) {
		t.Fatal("one nil CollectionID must not match")
	}
	// both nil
	a.CollectionID = nil
	if !sameSignedSession(a, b) {
		t.Fatal("both nil CollectionID must match")
	}
}

func TestSameSignedSession_differentID(t *testing.T) {
	a := baseSession()
	b := a
	b.ID = uuid.New()
	if sameSignedSession(a, b) {
		t.Fatal("different ID must not match")
	}
}

func TestSameSignedSession_differentObjectKey(t *testing.T) {
	a := baseSession()
	b := a
	b.objectKey = "different/key"
	if sameSignedSession(a, b) {
		t.Fatal("different objectKey must not match")
	}
}

func TestValidPartNumbers_valid(t *testing.T) {
	s := Session{PartsTotal: 5}
	if !validPartNumbers([]int{1, 2, 3}, s) {
		t.Fatal("valid sequential parts must be accepted")
	}
	if !validPartNumbers([]int{5}, s) {
		t.Fatal("single last part must be accepted")
	}
}

func TestValidPartNumbers_empty(t *testing.T) {
	s := Session{PartsTotal: 5}
	if validPartNumbers([]int{}, s) {
		t.Fatal("empty parts must be rejected")
	}
}

func TestValidPartNumbers_tooMany(t *testing.T) {
	s := Session{PartsTotal: 200}
	parts := make([]int, 101)
	for i := range parts {
		parts[i] = i + 1
	}
	if validPartNumbers(parts, s) {
		t.Fatal("more than 100 parts must be rejected")
	}
}

func TestValidPartNumbers_duplicate(t *testing.T) {
	s := Session{PartsTotal: 5}
	if validPartNumbers([]int{1, 1}, s) {
		t.Fatal("duplicate part numbers must be rejected")
	}
}

func TestValidPartNumbers_outOfRange(t *testing.T) {
	s := Session{PartsTotal: 5}
	if validPartNumbers([]int{0}, s) {
		t.Fatal("part 0 must be rejected")
	}
	if validPartNumbers([]int{6}, s) {
		t.Fatal("part beyond PartsTotal must be rejected")
	}
}
