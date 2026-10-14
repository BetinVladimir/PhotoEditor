package semanticproposal

import (
	"context"
	"errors"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestNativeStagePresenceStrictDecoding(t *testing.T) {
	for _, value := range []string{"absent", "present", "conflict"} {
		got, err := decodeNativeStagePresence(value)
		if err != nil || string(got) != value {
			t.Fatal(value, err)
		}
	}
	for _, value := range []string{"", "null", "missing", "ABSENT", " absent"} {
		got, err := decodeNativeStagePresence(value)
		if err == nil || got != "" {
			t.Fatal("invalid presence became authority", value)
		}
	}
}
func TestNativeStagePresenceCancelledBeforeIO(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	presence, err := (SQLTransferSourceReader{}).readNativeStagePresence(ctx, auth.Identity{}, detectorDraftPlan{}, "face.parse")
	if !errors.Is(err, context.Canceled) || presence != "" {
		t.Fatal("cancelled lookup granted absence", err)
	}
	presence, err = (SQLTransferSourceReader{}).readNativeStagePresence(context.Background(), auth.Identity{}, detectorDraftPlan{}, "face.parse")
	if err == nil || presence != "" {
		t.Fatal("missing infrastructure granted absence")
	}
}
