package main
import("context";"encoding/json";"errors";"testing";"github.com/jackc/pgx/v5")
type streamRows struct{*controlRows;owner *serialTx}
func(r *streamRows)Close(){r.owner.active=false;r.controlRows.Close()}
type serialTx struct{pgx.Tx;active bool;queries int;validations int;raw []byte}
func(t *serialTx)Query(_ context.Context,q string,_ ...any)(pgx.Rows,error){if t.active{return nil,errors.New("connection already has active result")};t.active=true;t.queries++;data:=[][]any{{"bulk_item_results"}};if t.queries>1{data=[][]any{{t.raw}}};return &streamRows{&controlRows{data:data},t},nil}
func(t *serialTx)QueryRow(_ context.Context,_ string,_ ...any)pgx.Row{return controlRow(func(d ...any)error{if t.active{return errors.New("nested query while Rows open")};t.validations++;*d[0].(*bool)=true;return nil})}
func TestWholeCaptureClosesStreamBeforeCanonicalQuery(t *testing.T){r:=bulkFixture(t);raw,_:=json.Marshal(r);tx:=&serialTx{raw:raw};snap,e:=capture(context.Background(),tx,fixture(t));if e!=nil||tx.active||tx.validations!=1||snap["bulk_item_results"]==""{t.Fatal("stream not closed before validation",e,tx.active,tx.validations)}}
