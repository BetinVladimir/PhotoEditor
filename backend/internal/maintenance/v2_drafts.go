package maintenance

import (
	"context"
	"errors"

	"photoeditor/backend/internal/artifactjson"
)

// V2DraftCleaner expires one unsealed, quota-backed V2 draft per cycle. The
// SQL function owns the race with graph seal and settles the run reservation.
type V2DraftCleaner struct{ DB artifactjson.RowQuerier }

func (c V2DraftCleaner) CleanOne(ctx context.Context) (bool, error) {
	if c.DB == nil {
		return false, errors.New("v2 draft cleanup database required")
	}
	var expired bool
	err := c.DB.QueryRow(ctx, "SELECT photo.expire_one_v2_draft()").Scan(&expired)
	return expired, err
}
