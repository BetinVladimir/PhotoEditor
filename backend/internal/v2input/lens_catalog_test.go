package v2input

import (
 "context"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/auth"
)

type lensCatalogRows struct { pgx.Rows; refs []Ref; index int; closed bool }
func(r *lensCatalogRows)Next()bool{r.index++;return r.index<=len(r.refs)}
func(r *lensCatalogRows)Close(){r.closed=true}
func(r *lensCatalogRows)Err()error{return nil}
func(r *lensCatalogRows)Scan(dest ...any)error{ref:=r.refs[r.index-1];*dest[0].(*uuid.UUID)=ref.ArtifactID;*dest[1].(*string)=ref.SHA256;return nil}
type lensCatalogDB struct { rows *lensCatalogRows; calls int }
func(d *lensCatalogDB)Query(_ context.Context,q string,args ...any)(pgx.Rows,error){d.calls++;if !strings.Contains(q,"list_v25_lens_profiles")||len(args)!=5{return nil,ErrInvalid};return d.rows,nil}
func TestLensCatalogRejectsMalformedRowsBeforeObjectIO(t *testing.T){
 who:=auth.Identity{UserID:uuid.New(),SessionID:uuid.New()};tenant,project:=uuid.New(),uuid.New()
 a:=Ref{uuid.MustParse("00000000-0000-4000-8000-000000000001"),strings.Repeat("a",64)}
 b:=Ref{uuid.MustParse("00000000-0000-4000-8000-000000000002"),strings.Repeat("b",64)}
 for _,refs:=range [][]Ref{{a,a},{b,a},{{uuid.Nil,strings.Repeat("c",64)}},{{a.ArtifactID,"bad"}}}{
  rows:=&lensCatalogRows{refs:refs};db:=&lensCatalogDB{rows:rows}
  _,err:=(LensCatalog{DB:db}).List(context.Background(),who,tenant,project,nil)
  if err!=ErrInvalid || !rows.closed{t.Fatal("malformed discovery row exposed",err)}
 }
 rows:=&lensCatalogRows{refs:[]Ref{a}};db:=&lensCatalogDB{rows:rows};after:=a.ArtifactID
 if _,err:=(LensCatalog{DB:db}).List(context.Background(),who,tenant,project,&after);err!=ErrInvalid || !rows.closed{t.Fatal("cursor replay exposed",err)}
}
func TestLensCatalogCannotExposeUnverifiedProfiles(t *testing.T){
 rows:=&lensCatalogRows{refs:[]Ref{{uuid.New(),strings.Repeat("a",64)}}};db:=&lensCatalogDB{rows:rows}
 page,err:=(LensCatalog{DB:db}).List(context.Background(),auth.Identity{UserID:uuid.New(),SessionID:uuid.New()},uuid.New(),uuid.New(),nil)
 if err==nil || len(page.Items)!=0 || page.Next!=nil || !rows.closed{t.Fatal("catalog trusted index without physical verification")}
}
