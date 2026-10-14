package v2optical

import (
 "context"
 "testing"
 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
)
func TestAffectedSourceMappingMatchesExactFrozenOccurrence(t *testing.T){
 p:=perspectiveIntent();frozen,_,err:=p.Compile();if err!=nil{t.Fatal(err)}
 producer,run:=uuid.New(),uuid.New()
 row:=negativeAffectedSource{Artifact:p.Base.Source.ArtifactID,SHA:p.Base.Source.SHA256,Producer:producer,Run:run}
 parents:=[]NegativeReplacementParent{{Original:producer,Kind:"selected_base"}}
 raw,_:=canonical.Marshal([]negativeAffectedSource{row})
 got,err:=matchNegativeAffectedSources(context.Background(),"optical.perspective",frozen,parents,raw)
 if err!=nil||len(got)!=1||got[0].Before!=p.Base||got[0].Producer.Run!=run{t.Fatal("exact source occurrence",err)}
 for _,mode:=range []string{"unknown_parent","extra_source","wrong_sha","retained","duplicate","null","unknown_field"}{t.Run(mode,func(t *testing.T){
  r:=row;ps:=append([]NegativeReplacementParent(nil),parents...);rows:=[]negativeAffectedSource{r}
  switch mode{case "unknown_parent":rows[0].Producer=uuid.New();case "extra_source":rows[0].Artifact=uuid.New();case "wrong_sha":rows[0].SHA="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";if rows[0].SHA==row.SHA{rows[0].SHA="bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"};case "retained":ps[0].Kind="retained";case "duplicate":rows=append(rows,r)}
  bad,_:=canonical.Marshal(rows);if mode=="null"{bad=[]byte("null")};if mode=="unknown_field"{bad=[]byte(`[{"unexpected":true}]`)}
  if _,err:=matchNegativeAffectedSources(context.Background(),"optical.perspective",frozen,ps,bad);err==nil{t.Fatal("invalid mapping accepted",mode)}
 })}
 if got,err:=matchNegativeAffectedSources(context.Background(),"optical.perspective",frozen,parents,[]byte("[]"));err!=nil||len(got)!=0{t.Fatal("control-only source mapping",err)}
}
