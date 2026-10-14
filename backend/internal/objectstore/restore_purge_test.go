package objectstore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

func TestObservePurgedTenantEveryNamespaceAndVersionFamily(t *testing.T) {
	for _, mode := range []string{"valid", "object", "delete_marker", "multipart", "original", "staging", "list_error", "nil_listing", "late_object", "guard", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			tenant := uuid.New()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			versionCalls, uploadCalls := 0, 0
			guard := func(ctx context.Context, id uuid.UUID) error {
				if id != tenant {
					t.Fatal("foreign scope")
				}
				if mode == "guard" {
					return errors.New("not isolated")
				}
				return ctx.Err()
			}
			versions := func(ctx context.Context, prefix string) <-chan minio.ObjectInfo {
				versionCalls++
				if mode == "nil_listing" {
					return nil
				}
				rows := make(chan minio.ObjectInfo, 1)
				if mode == "object" || mode == "delete_marker" || (mode == "original" && strings.HasPrefix(prefix, "originals/")) || (mode == "staging" && strings.HasPrefix(prefix, "staging/")) || (mode == "late_object" && versionCalls == 4) {
					rows <- minio.ObjectInfo{Key: prefix + "remaining", VersionID: "v1"}
				}
				if mode == "list_error" {
					rows <- minio.ObjectInfo{Err: errors.New("listing unavailable")}
				}
				close(rows)
				return rows
			}
			uploads := func(ctx context.Context, prefix string) <-chan minio.ObjectMultipartInfo {
				uploadCalls++
				rows := make(chan minio.ObjectMultipartInfo, 1)
				if mode == "multipart" {
					rows <- minio.ObjectMultipartInfo{Key: prefix + "remaining"}
				}
				close(rows)
				return rows
			}
			result, err := observePurgedTenant(ctx, "fixture-bucket", tenant, guard, versions, uploads)
			if mode == "valid" {
				if err != nil || result.Passes != 2 || versionCalls != 6 || uploadCalls != 6 || len(result.Prefixes) != 3 {
					t.Fatalf("incomplete observation %#v %v", result, err)
				}
			} else if err == nil || !result.ObservedAt.IsZero() {
				t.Fatal("storage remnants accepted")
			}
			if (mode == "guard" || mode == "cancelled") && versionCalls != 0 {
				t.Fatal("IO before guard")
			}
		})
	}
}

func TestObservePurgedTenantCancellationInterruptsStalledListing(t *testing.T){
 for _,stalled:=range []string{"versions","uploads"}{t.Run(stalled,func(t *testing.T){
  tenant:=uuid.New();ctx,cancel:=context.WithCancel(context.Background())
  entered:=make(chan struct{});done:=make(chan error,1)
  versions:=func(context.Context,string)<-chan minio.ObjectInfo{
   if stalled=="versions"{close(entered);return make(chan minio.ObjectInfo)}
   rows:=make(chan minio.ObjectInfo);close(rows);return rows
  }
  uploads:=func(context.Context,string)<-chan minio.ObjectMultipartInfo{
   if stalled=="uploads"{close(entered);return make(chan minio.ObjectMultipartInfo)}
   rows:=make(chan minio.ObjectMultipartInfo);close(rows);return rows
  }
  go func(){_,err:=observePurgedTenant(ctx,"fixture-bucket",tenant,func(context.Context,uuid.UUID)error{return nil},versions,uploads);done<-err}()
  select{case <-entered:case <-time.After(time.Second):cancel();t.Fatal("listing not entered")}
  cancel()
  select{case err:=<-done:if !errors.Is(err,context.Canceled){t.Fatal("wrong cancellation",err)}
   case <-time.After(time.Second):t.Fatal("stalled listing ignored cancellation")}
 })}
}
