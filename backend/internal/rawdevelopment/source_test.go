package rawdevelopment

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "io"
 "strings"
 "testing"
 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/objectstore"
)

type rawReportRow struct {raw []byte;mode string;number int}
func (r rawReportRow) Scan(dst ...any)error{
 if r.mode=="missing" || r.mode=="revoked"&&r.number==2{return pgx.ErrNoRows}
 *dst[0].(*string)="bucket";*dst[1].(*string)="reports/qualified.json"
 *dst[2].(*string)="version1";*dst[3].(*int64)=int64(len(r.raw));*dst[4].(*string)="application/json"
 if r.mode=="foreign bucket" {*dst[0].(*string)="other"}
 if r.mode=="changed version"&&r.number==2 {*dst[2].(*string)="version2"}
 return nil
}
type rawReportDB struct {raw []byte;mode string;calls int}
func (d *rawReportDB) QueryRow(ctx context.Context,q string,args ...any)pgx.Row{d.calls++;return rawReportRow{d.raw,d.mode,d.calls}}
type rawReportStore struct {raw []byte;calls int;cancel context.CancelFunc;onOpen func()}
func (s *rawReportStore) Open(ctx context.Context,obj objectstore.Object)(io.ReadCloser,error){s.calls++;if s.onOpen!=nil{s.onOpen()};if s.cancel!=nil{s.cancel()};return io.NopCloser(strings.NewReader(string(s.raw))),nil}

func TestRAWCapabilitySourceReadsExactReportAndRechecksAuthority(t *testing.T){
 report,expected:=rawCapabilityFixture();raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(raw);digest:=hex.EncodeToString(sum[:])
 for _,mode:=range []string{"valid","missing","foreign bucket","changed version","revoked","cancelled","corrupt bytes"}{
  t.Run(mode,func(t *testing.T){
   ctx,cancel:=context.WithCancel(context.Background());defer cancel()
   db:=&rawReportDB{raw:raw,mode:mode};store:=&rawReportStore{raw:raw}
   if mode=="cancelled"{store.cancel=cancel}
   if mode=="corrupt bytes"{store.raw=append([]byte(nil),raw...);store.raw[len(store.raw)-1]=' '}
   source:=Source{Objects:store,Bucket:"bucket",ReportID:uuid.New(),ReportSHA:digest,DecoderVersion:"rawpy-0.27.1/libraw-0.21.4",Expected:expected}
   capability,err:=source.Load(ctx,db,uuid.New(),uuid.New(),uuid.New(),uuid.New())
   if mode=="valid"{
    if err!=nil || capability==nil || db.calls!=2 || store.calls!=1{t.Fatal("qualified source failed",err)}
   }else if err==nil || capability!=nil{t.Fatal("invalid authority accepted")}
   if (mode=="missing"||mode=="foreign bucket")&&store.calls!=0{t.Fatal("invalid authority reached storage")}
  })
 }
}

// The physical source must bound reads by the locked descriptor, close every
// opened stream and refuse length substitution before report verification.
type countedRAWReader struct { remaining int64; read int64; closed bool }
func (r *countedRAWReader) Read(p []byte)(int,error){
 if r.remaining==0{return 0,io.EOF}
 n:=len(p);if int64(n)>r.remaining{n=int(r.remaining)}
 for i:=0;i<n;i++{p[i]='x'}
 r.remaining-=int64(n);r.read+=int64(n);return n,nil
}
func (r *countedRAWReader) Close()error{r.closed=true;return nil}
type streamRAWStore struct {reader io.ReadCloser;cancel context.CancelFunc}
func (s streamRAWStore) Open(context.Context,objectstore.Object)(io.ReadCloser,error){
 if s.cancel!=nil{s.cancel()};return s.reader,nil
}
func TestRAWSourceBoundsPhysicalReadsAndCloses(t *testing.T){
 report,expected:=rawCapabilityFixture();raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(raw)
 for _,mode:=range []string{"oversized","truncated","cancelled","nil stream"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  reader:=&countedRAWReader{remaining:64*1024*1024}
  store:=streamRAWStore{reader:reader}
  if mode=="truncated"{reader.remaining=int64(len(raw))-1}
  if mode=="cancelled"{store.cancel=cancel}
  if mode=="nil stream"{store.reader=nil}
  db:=&rawReportDB{raw:raw}
  source:=Source{Objects:store,Bucket:"bucket",ReportID:uuid.New(),ReportSHA:hex.EncodeToString(sum[:]),DecoderVersion:"rawpy-0.27.1/libraw-0.21.4",Expected:expected}
  capability,err:=source.Load(ctx,db,uuid.New(),uuid.New(),uuid.New(),uuid.New())
  if err==nil||capability!=nil{t.Fatal("invalid stream accepted")}
  if db.calls!=1{t.Fatal("invalid body reached authority recheck")}
  if mode!="nil stream"&&!reader.closed{t.Fatal("stream leaked")}
  if reader.read>int64(len(raw))+1{t.Fatal("read exceeded locked descriptor")}
  if mode=="cancelled"&&reader.read!=0{t.Fatal("cancelled request read body")}
 })}
}

func TestRAWAdmissionBindsPolicyToCurrentQualification(t *testing.T){
 report,expected:=rawCapabilityFixture();raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
 sum:=sha256.Sum256(raw)
 for _,mode:=range []string{"valid","legacy","invalid policy","foreign decoder","revoked","cancelled"}{t.Run(mode,func(t *testing.T){
  ctx,cancel:=context.WithCancel(context.Background());defer cancel()
  policy:=&Policy{1,"dcb","auto",2,"reconstruct","rawpy-0.27.1/libraw-0.21.4"}
  db:=&rawReportDB{raw:raw};store:=&rawReportStore{raw:raw}
  source:=Source{Objects:store,Bucket:"bucket",ReportID:uuid.New(),ReportSHA:hex.EncodeToString(sum[:]),DecoderVersion:policy.DecoderVersion,Expected:expected}
  switch mode{
  case "legacy":policy=nil
  case "invalid policy":policy.ExposureEV=4
  case "foreign decoder":policy.DecoderVersion="rawpy-0.27.0/libraw-0.21.4"
  case "revoked":db.mode="revoked"
  case "cancelled":cancel()
  }
  admitted,err:=source.Admit(ctx,db,uuid.New(),uuid.New(),uuid.New(),uuid.New(),policy)
  switch mode{
  case "valid":
   if err!=nil||admitted==nil||*admitted!=*policy||admitted==policy{t.Fatal("policy not independently bound",err)}
   policy.Demosaic="changed";if admitted.Demosaic!="dcb"{t.Fatal("policy ownership leaked")}
   if db.calls!=2||store.calls!=1{t.Fatal("qualification not rechecked")}
  case "legacy":if err!=nil||admitted!=nil||db.calls!=0||store.calls!=0{t.Fatal("legacy path changed",err)}
  default:
   if err==nil||admitted!=nil{t.Fatal("unqualified policy admitted")}
   if (mode=="invalid policy"||mode=="cancelled")&&(db.calls!=0||store.calls!=0){t.Fatal("invalid request reached adapters")}
  }
 })}
}

func TestRAWSourceOwnsQualificationPinsAcrossObjectIO(t *testing.T){
 for _,mode:=range []string{"mutate-allowlists","remove-required-check"}{t.Run(mode,func(t *testing.T){
  report,expected:=rawCapabilityFixture()
  if mode=="remove-required-check"{expected.RequiredChecks=[]string{"deployment.extra.required"}}
  raw,err:=canonical.Marshal(report);if err!=nil{t.Fatal(err)}
  sum:=sha256.Sum256(raw)
  store:=&rawReportStore{raw:raw,onOpen:func(){
   if mode=="remove-required-check"{expected.RequiredChecks[0]=requiredChecks[0];return}
   for value:=range expected.TestFixtureSHAs{delete(expected.TestFixtureSHAs,value)}
   for value:=range expected.EvidenceSHAs{delete(expected.EvidenceSHAs,value)}
  }}
  source:=Source{Objects:store,Bucket:"bucket",ReportID:uuid.New(),ReportSHA:hex.EncodeToString(sum[:]),DecoderVersion:"rawpy-0.27.1/libraw-0.21.4",Expected:expected}
  db:=&rawReportDB{raw:raw}
  capability,err:=source.Load(context.Background(),db,uuid.New(),uuid.New(),uuid.New(),uuid.New())
  if mode=="mutate-allowlists"{if err!=nil||capability==nil||db.calls!=2{t.Fatal("caller mutation changed pinned authority",err,db.calls)}}else if err==nil||capability!=nil||db.calls!=1{t.Fatal("caller removed mandatory qualification check",err,db.calls)}
 })}
}
