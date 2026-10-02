package collection

import (
	"context"
	"errors"
	"time"

	"photoeditor/backend/internal/artifactjson"
)

type BulkProcessor struct{ DB artifactjson.RowQuerier }

func (p BulkProcessor) ProcessOne(ctx context.Context) (bool, error) {
	if p.DB == nil {
		return false, errors.New("bulk database unavailable")
	}
	call, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var changed bool
	err := p.DB.QueryRow(call, "SELECT photo.enqueue_bulk_chunk()").Scan(&changed)
	return changed, err
}
