package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/pagination"
	"testing"
	"time"
)

func TestTargetReaderRejectsCrossScopeCursorBeforeDatabase(t *testing.T) {
	tid, intent, user := uuid.New(), uuid.New(), uuid.New()
	codec := pagination.Codec{Key: make([]byte, 32), Now: func() time.Time { return time.Unix(1000, 0) }}
	hash, _ := canonical.Hash(map[string]any{"intent_id": intent.String()})
	for _, changed := range []string{"tenant", "principal", "intent", "sort"} {
		scope := pagination.Cursor{Tenant: tid.String(), Principal: user.String(), FilterHash: hash, Sort: "semantic_target:created_at,id", LastID: uuid.NewString(), LastPosition: time.Now().Format(time.RFC3339Nano)}
		switch changed {
		case "tenant":
			scope.Tenant = uuid.NewString()
		case "principal":
			scope.Principal = uuid.NewString()
		case "intent":
			scope.FilterHash = "foreign"
		case "sort":
			scope.Sort = "asset:created_at,id"
		}
		raw, err := codec.Encode(scope)
		if err != nil {
			t.Fatal(err)
		}
		_, err = (TargetReader{Cursors: codec}).List(context.Background(), auth.Identity{UserID: user}, tid, intent, 50, raw)
		if err == nil || err.Error() == "DEPENDENCY_UNAVAILABLE" {
			t.Fatalf("foreign %s cursor reached database: %v", changed, err)
		}
	}
}
