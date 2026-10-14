package interchange

import (
 "context"
 "testing"
 "photoeditor/backend/internal/v2input"
)
type seriesAuthorityStub struct{calls int;denyAt int}
func(s *seriesAuthorityStub)QualifyExportSeries(context.Context,v2input.Entry,[]byte)error{s.calls++;if s.calls==s.denyAt{return v2input.ErrInvalid};return nil}
func TestSeriesProducerValidatorRechecksAuthority(t *testing.T){
 for _,deny:=range []int{0,1,2}{t.Run(string(rune('0'+deny)),func(t *testing.T){
  authority:=&seriesAuthorityStub{denyAt:deny};other:=&seriesChildlessPolicy{}
  validator:=ExportSeriesProducerValidator{Authority:authority,Other:other}
  _,err:=validator.ExportDocumentChildren(context.Background(),v2input.Entry{Contract:"SeriesCorrections"},[]byte(`{}`))
  if (err==nil)!=(deny==0){t.Fatal("producer refusal bypassed",err)}
  expected:=2;if deny==1{expected=1};if authority.calls!=expected{t.Fatal("authority lifecycle",authority.calls)}
  if deny==1&&other.calls!=0{t.Fatal("unqualified series reached policy")}
 })}
}
func TestSeriesSQLAuthorityRequiresDatabase(t *testing.T){
 if err:=(SQLExportDocumentCatalog{}).QualifyExportSeries(context.Background(),v2input.Entry{Contract:"SeriesCorrections"},[]byte(`{}`));err!=v2input.ErrInvalid{t.Fatal(err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=(SQLExportDocumentCatalog{}).QualifyExportSeries(ctx,v2input.Entry{},nil);err!=context.Canceled{t.Fatal(err)}
}

type seriesChildlessPolicy struct{calls int}
func(s *seriesChildlessPolicy)ExportDocumentChildren(context.Context,v2input.Entry,[]byte)([]v2input.Entry,error){s.calls++;return nil,nil}
