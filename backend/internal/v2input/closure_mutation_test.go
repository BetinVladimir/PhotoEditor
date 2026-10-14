package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"testing"
)

type mutatingClosurePublisher struct{}

func (mutatingClosurePublisher) Publish(_ context.Context, _ uuid.UUID, _ int, _ string, body []byte) (Ref, error) {
	// A syntactically valid JSON modification cannot redefine compiler intent.
	for i, b := range body {
		if b == '1' {
			body[i] = '2'
			break
		}
	}
	hash := sha256.Sum256(body)
	return Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(hash[:])}, nil
}
func TestClosurePublisherCannotRedefineCanonicalBody(t *testing.T) {
	_, err := publish(context.Background(), uuid.New(), 0, "InputClosureChunk", map[string]int{"schema_version": 1}, mutatingClosurePublisher{})
	if !errors.Is(err, ErrInvalid) {
		t.Fatal("mutated publication accepted", err)
	}
}
