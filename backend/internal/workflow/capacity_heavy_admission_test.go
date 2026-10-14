package workflow

import (
 "context"
 "errors"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5"
)

type heavyAdmissionTx struct { pgx.Tx; calls int; allowed bool; err error; pin string }
func (tx *heavyAdmissionTx) QueryRow(_ context.Context,q string,args ...any) pgx.Row {
 tx.calls++
 if q!="SELECT photo.capacity_heavy_admission_available($1)"||len(args)!=1||args[0]!=tx.pin { return heavyAdmissionRow{err:errors.New("foreign admission query")} }
 return heavyAdmissionRow{allowed:tx.allowed,err:tx.err}
}
type heavyAdmissionRow struct {allowed bool;err error}
func(r heavyAdmissionRow) Scan(dest ...any)error {if r.err!=nil{return r.err};*dest[0].(*bool)=r.allowed;return nil}

func TestHeavyAdmissionUsesAuthoritativeResourceClass(t *testing.T){
 pin:=strings.Repeat("a",64)
 for _,allowed:=range []bool{false,true}{
  tx:=&heavyAdmissionTx{allowed:allowed,pin:pin}
  got,err:=capacityHeavyAdmission(context.Background(),tx,pin,"gpu")
  if err!=nil||got!=allowed||tx.calls!=1{t.Fatal(got,err,tx.calls)}
 }
 for _,class:=range []string{"cpu","media","archive","collection","mail"}{
  tx:=&heavyAdmissionTx{}
  got,err:=capacityHeavyAdmission(context.Background(),tx,"",class)
  if err!=nil||!got||tx.calls!=0{t.Fatal(class,got,err)}
 }
 tx:=&heavyAdmissionTx{pin:pin}
 for _,invalid:=range []string{"","bad"}{
  if got,err:=capacityHeavyAdmission(context.Background(),tx,invalid,"gpu");err==nil||got||tx.calls!=0{t.Fatal("missing GPU authority accepted")}
 }
 if got,err:=capacityHeavyAdmission(context.Background(),tx,pin,"unknown");err==nil||got||tx.calls!=0{t.Fatal("unknown class accepted")}
 sentinel:=errors.New("SQL failure");tx.err=sentinel
 if got,err:=capacityHeavyAdmission(context.Background(),tx,pin,"gpu");got||!errors.Is(err,sentinel){t.Fatal(got,err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel();tx.calls=0
 if got,err:=capacityHeavyAdmission(ctx,tx,pin,"gpu");got||!errors.Is(err,context.Canceled)||tx.calls!=0{t.Fatal(got,err)}
}
