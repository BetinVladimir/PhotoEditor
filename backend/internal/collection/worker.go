package collection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
)

var ErrPoisonBulkCommand = errors.New("poison bulk command")

type BulkCommand struct {
	SchemaVersion int       `json:"schema_version"`
	EventID       uuid.UUID `json:"event_id"`
	BulkID        uuid.UUID `json:"bulk_id"`
}

type MediaRetryCommand struct {
	SchemaVersion  int       `json:"schema_version"`
	EventID        uuid.UUID `json:"event_id"`
	BulkID         uuid.UUID `json:"bulk_id"`
	ItemID         uuid.UUID `json:"item_id"`
	AssetVersionID uuid.UUID `json:"asset_version_id"`
	Variants       []string  `json:"variants"`
}

func parseStrict(body []byte, target any) error {
	if len(body) == 0 || len(body) > 65536 {
		return ErrPoisonBulkCommand
	}
	encoded, err := canonical.Bytes(body)
	if err != nil {
		return ErrPoisonBulkCommand
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return ErrPoisonBulkCommand
	}
	if err = decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return ErrPoisonBulkCommand
	}
	return nil
}

type BulkWorker struct{ DB artifactjson.RowQuerier }

func ParseBulkCommand(body []byte) (BulkCommand, error) {
	var command BulkCommand
	if err := parseStrict(body, &command); err != nil {
		return command, err
	}
	if command.SchemaVersion != 1 || command.EventID == uuid.Nil || command.BulkID == uuid.Nil {
		return command, ErrPoisonBulkCommand
	}
	return command, nil
}

func ParseMediaRetryCommand(body []byte) (MediaRetryCommand, error) {
	var command MediaRetryCommand
	if err := parseStrict(body, &command); err != nil {
		return command, err
	}
	if command.SchemaVersion != 1 || command.EventID == uuid.Nil || command.BulkID == uuid.Nil || command.ItemID == uuid.Nil || command.AssetVersionID == uuid.Nil || len(command.Variants) < 1 || len(command.Variants) > 2 {
		return command, ErrPoisonBulkCommand
	}
	seen := map[string]bool{}
	for _, variant := range command.Variants {
		if seen[variant] || (variant != "thumbnail" && variant != "preview") {
			return command, ErrPoisonBulkCommand
		}
		seen[variant] = true
	}
	return command, nil
}

func (w BulkWorker) Process(ctx context.Context, body []byte) (bool, error) {
	if w.DB == nil {
		return false, errors.New("bulk worker database unavailable")
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var changed bool
	if command, err := ParseBulkCommand(body); err == nil {
		err = w.DB.QueryRow(call, "SELECT photo.process_bulk_chunk($1,$2)", command.BulkID, command.EventID).Scan(&changed)
		return changed, err
	}
	command, err := ParseMediaRetryCommand(body)
	if err != nil {
		return false, err
	}
	err = w.DB.QueryRow(call, "SELECT photo.accept_media_retry_command($1,$2,$3,$4,$5)", command.EventID, command.BulkID, command.ItemID, command.AssetVersionID, command.Variants).Scan(&changed)
	return changed, err
}
