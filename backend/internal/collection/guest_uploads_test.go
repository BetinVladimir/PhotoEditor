package collection

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/pagination"
)

func TestGuestUploadListRejectsUnboundedAndForeignCursorsBeforeDatabase(t *testing.T) {
	tid, cid, actor := uuid.New(), uuid.New(), uuid.New()
	codec := pagination.Codec{Key: []byte(strings.Repeat("g",32))}
	service := Service{Cursors: codec}
	who := auth.Identity{UserID: actor}
	for _, limit := range []int{-1,0,101} {
		_, err := service.ListGuestUploads(context.Background(),who,tid,cid,limit,"")
		if err == nil || domain.AsError(err).Code != "VALIDATION_FAILED" { t.Fatalf("unbounded limit %d: %v",limit,err) }
	}
	base := pagination.Cursor{Tenant:tid.String(),Principal:actor.String(),FilterHash:cid.String(),Sort:"guest-upload:created_at,id",LastID:uuid.NewString(),LastPosition:time.Now().UTC().Format(time.RFC3339Nano)}
	for name, mutate := range map[string]func(*pagination.Cursor){
		"tenant":func(c *pagination.Cursor){c.Tenant=uuid.NewString()},
		"actor":func(c *pagination.Cursor){c.Principal=uuid.NewString()},
		"collection":func(c *pagination.Cursor){c.FilterHash=uuid.NewString()},
		"sort":func(c *pagination.Cursor){c.Sort="section:position,id"},
		"nil upload":func(c *pagination.Cursor){c.LastID=uuid.Nil.String()},
		"invalid time":func(c *pagination.Cursor){c.LastPosition="not-a-time"},
	} {
		t.Run(name,func(t *testing.T){
			cursor:=base;mutate(&cursor)
			raw,err:=codec.Encode(cursor);if err!=nil{t.Fatal(err)}
			_,err=service.ListGuestUploads(context.Background(),who,tid,cid,20,raw)
			if err==nil||domain.AsError(err).Code!="INVALID_CURSOR"{t.Fatalf("foreign/invalid cursor: %v",err)}
		})
	}
}
