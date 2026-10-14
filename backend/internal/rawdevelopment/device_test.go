package rawdevelopment

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2capture"
)
type rawDeviceReportDB struct{rawReportDB;t *testing.T;principal v2capture.DevicePrincipal;shoot,collection uuid.UUID}
func (d *rawDeviceReportDB) QueryRow(ctx context.Context,q string,args ...any)pgx.Row{
 if !strings.Contains(q,"v25_raw_device_qualification_object")||len(args)!=9||args[0]!=d.principal.TenantID||args[1]!=d.principal.ProjectID||args[2]!=d.principal.DeviceID||args[3]!=d.principal.CredentialEpoch||args[4]!=d.shoot||args[5]!=d.collection{d.t.Fatal("device authority substitution")}
 return d.rawReportDB.QueryRow(ctx,q,args...)
}
func TestRAWDeviceAdmissionUsesDistinctCurrentAuthority(t *testing.T){
 report,expected:=rawCapabilityFixture();raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)};sum:=sha256.Sum256(raw)
 for _,mode:=range []string{"valid","revoked","invalid epoch","foreign decoder"}{t.Run(mode,func(t *testing.T){
  principal:=v2capture.DevicePrincipal{TenantID:uuid.New(),ProjectID:uuid.New(),DeviceID:uuid.New(),CredentialEpoch:2}
  db:=&rawDeviceReportDB{rawReportDB:rawReportDB{raw:raw},t:t,principal:principal,shoot:uuid.New(),collection:uuid.New()}
  if mode=="revoked"{db.mode="revoked"};if mode=="invalid epoch"{principal.CredentialEpoch=0}
  store:=&rawReportStore{raw:raw};source:=Source{Objects:store,Bucket:"bucket",ReportID:uuid.New(),ReportSHA:hex.EncodeToString(sum[:]),DecoderVersion:"rawpy-0.27.1/libraw-0.21.4",Expected:expected}
  policy:=&Policy{1,"dcb","auto",2,"blend",source.DecoderVersion};if mode=="foreign decoder"{policy.DecoderVersion="rawpy-0.27.0/libraw-0.21.4"}
  out,err:=source.AdmitDevice(context.Background(),db,principal,db.shoot,db.collection,policy)
  if mode=="valid"{if err!=nil||out==nil||*out!=*policy||out==policy||db.calls!=2{t.Fatal("device qualification failed",err)}}else if err==nil||out!=nil{t.Fatal("unqualified device accepted")}
  if mode=="invalid epoch"&&(store.calls!=0||db.calls!=0){t.Fatal("invalid device reached adapters")}
 })}
}
