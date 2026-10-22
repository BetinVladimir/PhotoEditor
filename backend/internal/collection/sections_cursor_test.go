package collection

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
)

// A correctly signed cursor is still untrusted input. Scope substitution and
// invalid seek tuples must be rejected before any tenant database access.
func TestListSectionsRejectsSignedForeignOrInvalidSeekCursor(t *testing.T) {
	tenantID, collectionID, actorID := uuid.New(), uuid.New(), uuid.New()
	hash, err := canonical.Hash(map[string]string{"collection_id": collectionID.String()})
	if err != nil { t.Fatal(err) }
	codec := pagination.Codec{Key: []byte(strings.Repeat("s", 32))}
	base := pagination.Cursor{Tenant: tenantID.String(), Principal: actorID.String(), FilterHash: hash, Sort: "section:position,id", LastID: uuid.New().String(), LastPosition: "0"}
	cases := map[string]func(*pagination.Cursor){
		"tenant": func(c *pagination.Cursor) { c.Tenant = uuid.New().String() },
		"actor": func(c *pagination.Cursor) { c.Principal = uuid.New().String() },
		"collection": func(c *pagination.Cursor) { c.FilterHash = "another-collection" },
		"sort": func(c *pagination.Cursor) { c.Sort = "item:position,id" },
		"nil section": func(c *pagination.Cursor) { c.LastID = uuid.Nil.String() },
		"invalid section": func(c *pagination.Cursor) { c.LastID = "invalid" },
		"negative position": func(c *pagination.Cursor) { c.LastPosition = "-1" },
		"unsafe position": func(c *pagination.Cursor) { c.LastPosition = "9007199254740992" },
		"fractional position": func(c *pagination.Cursor) { c.LastPosition = "1.5" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cursor := base
			mutate(&cursor)
			raw, err := codec.Encode(cursor)
			if err != nil { t.Fatal(err) }
			_, err = (Service{Cursors: codec}).ListSections(context.Background(), auth.Identity{UserID: actorID}, tenantID, collectionID, 20, raw)
			if err == nil || domain.AsError(err).Code != "INVALID_CURSOR" {
				t.Fatalf("expected INVALID_CURSOR before database access, got %v", err)
			}
		})
	}
}
