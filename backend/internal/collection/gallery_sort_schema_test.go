package collection

import (
 "context"
 "errors"
 "strings"
 "testing"
 "time"

 "github.com/jackc/pgx/v5"
)

type gallerySchemaRow struct{ ready bool; err error; after func() }
func(r gallerySchemaRow)Scan(dst ...any)error{if r.after!=nil{r.after()};*(dst[0].(*bool))=r.ready;return r.err}
type gallerySchemaReader struct{ready bool; err error; after func(); calls int; query string; bounded bool}
func(r *gallerySchemaReader)QueryRow(ctx context.Context,q string,_ ...any)pgx.Row{
 r.calls++;r.query=q;deadline,ok:=ctx.Deadline();r.bounded=ok&&time.Until(deadline)<=5*time.Second
 return gallerySchemaRow{r.ready,r.err,r.after}
}
func TestGallerySortStartupRequiresInstalledAuthorityAndOrdering(t *testing.T){
 for _,ready:=range []bool{false,true}{
  r:=&gallerySchemaReader{ready:ready}
  err:=RequireGallerySort(context.Background(),r)
  if (err==nil)!=ready||!r.bounded||r.calls!=1{t.Fatalf("ready=%v bounded=%v calls=%d err=%v",ready,r.bounded,r.calls,err)}
  for _,fragment:=range []string{"NOT p.prosecdef","a.grantee NOT IN","b.source_sha=original.sha256","r.content_sha=b.source_sha","ordering.sort_ordinal","photo.adopt_ingest_image"}{
   if !strings.Contains(r.query,fragment){t.Fatalf("missing installed contract %s",fragment)}
  }
 }
}
func TestGallerySortStartupPreservesCancellationAndDatabaseFailure(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 r:=&gallerySchemaReader{ready:true}
 if err:=RequireGallerySort(ctx,r);!errors.Is(err,context.Canceled)||r.calls!=0{t.Fatalf("err=%v calls=%d",err,r.calls)}
 ctx,cancel=context.WithCancel(context.Background());defer cancel()
 r=&gallerySchemaReader{ready:true,after:cancel}
 if err:=RequireGallerySort(ctx,r);!errors.Is(err,context.Canceled){t.Fatal(err)}
 failure:=errors.New("catalog unavailable");r=&gallerySchemaReader{err:failure}
 if err:=RequireGallerySort(context.Background(),r);!errors.Is(err,failure){t.Fatal(err)}
 if err:=RequireGallerySort(context.Background(),nil);err==nil{t.Fatal("nil database accepted")}
}
