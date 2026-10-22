package workflow

import (
 "encoding/json"
 "strings"
 "testing"
)

func TestSeriesAggregateFullSnapshotPreservesQualificationPins(t *testing.T){
 pin:=strings.Repeat("a",64);base,err:=applySeriesBalanceProfile(1024,pin,pin,pin);if err!=nil{t.Fatal(err)}
 packs:=make([]applyOutput,63);for i:=range packs{packs[i].Artifact.Bytes=900000}
 raw,err:=applySeriesAggregateResources(base,packs,2048);if err!=nil{t.Fatal(err)}
 var before,after map[string]json.RawMessage;_ = json.Unmarshal(base,&before);_ = json.Unmarshal(raw,&after)
 for _,key:=range []string{"model_lock_id","model_lock_sha","runtime_lock_sha","device_profile_sha"}{if string(before[key])!=string(after[key]){t.Fatalf("qualification %s lost",key)}}
 estimate,err:=ParseResourceEstimate(raw);if err!=nil{t.Fatal(err)}
 if estimate.OutputBytes!=8<<20||estimate.ScratchBytes<63*900000+(8<<20){t.Fatal("full snapshot document budget undercounted")}
 packs=append(packs,applyOutput{});if _,err=applySeriesAggregateResources(base,packs,2048);err==nil{t.Fatal("oversized pack inventory accepted")}
}

func TestSeriesPlanHasBoundedPackBetweenMeasurementAndAggregate(t *testing.T){
 plan,err:=CompileApplyV4Plan(json.RawMessage(`{"mode":"fixed","extensions":{"series":{"enabled":true}}}`),5000);if err!=nil{t.Fatal(err)}
 index:=map[string]int{};for i,s:=range plan{index[s.Phase]=i}
 if index["series_pack"]!=index["series_measure"]+1||index["series_balance"]!=index["series_pack"]+1||plan[index["series_pack"]].Cardinality!="group"{t.Fatal("series pack ordering/cardinality lost")}
}
