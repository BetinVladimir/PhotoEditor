package v2draft

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2portrait"
	"strings"
	"testing"
)

func TestLocalPortraitRecoveryConstructor(t *testing.T) {
	scheduler, catalog := &pgxpool.Pool{}, &pgxpool.Pool{}
	publisher := localTaskPublisherFunc(func(context.Context, artifactjson.Scope, []byte) (artifactjson.Ref, error) {
		t.Fatal("constructor performed IO")
		return artifactjson.Ref{}, nil
	})
	capacity, _ := v2portrait.LocalPortraitResources(400, 300)
	service, err := NewSQLLocalPortraitRecoveryService(scheduler, catalog, "photos", closureFixture{}, publisher, strings.Repeat("a", 64), capacity)
	if err != nil || service.Owners.(SQLLocalPortraitOwnerScope).DB != scheduler || service.Factory.(SQLLocalPortraitRecoveryFactory).CatalogDB != catalog || service.Capacity != capacity {
		t.Fatal("incorrect service assembly", err)
	}
	for _, invalid := range []v2portrait.LocalResourceEstimate{{}, {PixelCount: 1, RAMBytes: 1, ScratchBytes: 1, OutputBytes: 1, VRAMBytes: -1}, {PixelCount: 1, RAMBytes: 9007199254740992, ScratchBytes: 1, OutputBytes: 1}} {
		result, err := NewSQLLocalPortraitRecoveryService(scheduler, catalog, "photos", closureFixture{}, publisher, strings.Repeat("a", 64), invalid)
		if err == nil || result.Owners != nil {
			t.Fatal("invalid capacity configured")
		}
	}
	if result, err := NewSQLLocalPortraitRecoveryService(scheduler, nil, "photos", closureFixture{}, publisher, strings.Repeat("a", 64), capacity); err == nil || result.Owners != nil {
		t.Fatal("missing API pool configured")
	}
	if result, err := NewSQLLocalPortraitRecoveryService(scheduler, catalog, "photos", closureFixture{}, publisher, "invalid", capacity); err == nil || result.Owners != nil {
		t.Fatal("invalid runtime configured")
	}
}
