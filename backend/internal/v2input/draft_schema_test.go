package v2input

import (
 "context"
 "errors"
 "testing"
 "github.com/jackc/pgx/v5"
)

type draftSchemaRow struct {ready bool;err error;after func()}
func(r draftSchemaRow)Scan(dest ...any)error{if r.after!=nil{r.after()};if r.err!=nil{return r.err};*(dest[0].(*bool))=r.ready;return nil}
type draftSchemaDB struct{row draftSchemaRow;calls int}
func(d *draftSchemaDB)QueryRow(context.Context,string,...any)pgx.Row{d.calls++;return d.row}
func TestDraftSchemaRefusesOldOrMissingInstallation(t *testing.T){
 db:=&draftSchemaDB{}
 if RequireDraftEdgeUUID(context.Background(),db)==nil{t.Fatal("old schema accepted")}
 db.row.ready=true;if err:=RequireDraftEdgeUUID(context.Background(),db);err!=nil{t.Fatal(err)}
 failure:=errors.New("catalog unavailable");db.row.err=failure
 if !errors.Is(RequireDraftEdgeUUID(context.Background(),db),failure){t.Fatal("catalog failure hidden")}
}
func TestDraftSchemaCancellationBeforeAndAfterCatalogRead(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();db:=&draftSchemaDB{row:draftSchemaRow{ready:true}}
 if !errors.Is(RequireDraftEdgeUUID(ctx,db),context.Canceled)||db.calls!=0{t.Fatal("cancelled startup accessed database")}
 ctx,cancel=context.WithCancel(context.Background());defer cancel();db.row.after=cancel
 if !errors.Is(RequireDraftEdgeUUID(ctx,db),context.Canceled){t.Fatal("late probe accepted")}
}
