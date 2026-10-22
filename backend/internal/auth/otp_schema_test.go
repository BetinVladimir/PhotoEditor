package auth

import (
 "context"
 "errors"
 "testing"
 "time"
 "github.com/jackc/pgx/v5"
)
type otpSchemaRow struct{ ready bool; err error }
func(r otpSchemaRow) Scan(dest ...any)error{if r.err!=nil{return r.err};*(dest[0].(*bool))=r.ready;return nil}
type otpSchemaDB struct{row otpSchemaRow;bounded bool}
func(d *otpSchemaDB) QueryRow(ctx context.Context,_ string,_ ...any)pgx.Row{deadline,ok:=ctx.Deadline();d.bounded=ok&&time.Until(deadline)<=5*time.Second;return d.row}
func TestOTPReceiptStartupRequiresInstalledSchema(t *testing.T){
 for _,tc:=range []struct{name string;row otpSchemaRow;wantError bool}{
  {"current",otpSchemaRow{ready:true},false},
  {"old-schema-or-missing-role",otpSchemaRow{},true},
  {"database-error",otpSchemaRow{err:errors.New("unavailable")},true},
 }{t.Run(tc.name,func(t *testing.T){db:=&otpSchemaDB{row:tc.row};err:=RequireOTPReceiptSchema(context.Background(),db);if (err!=nil)!=tc.wantError{t.Fatal("unexpected startup result",err)};if !db.bounded{t.Fatal("unbounded startup query")}})}
 if err:=RequireOTPReceiptSchema(context.Background(),nil);err==nil{t.Fatal("missing database accepted")}
}
