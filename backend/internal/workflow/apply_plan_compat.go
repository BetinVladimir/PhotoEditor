package workflow

import (
 "context"
 "encoding/json"
 "errors"
 "strings"
)

type applyFrozenPlan struct{Version int `json:"version"`;Stages []ApplyV4Stage `json:"stages"`;Config json.RawMessage `json:"config"`}

func applyFrozenStyle(config json.RawMessage)(json.RawMessage,error){
 var cfg struct{Style json.RawMessage `json:"style_config"`;Graph json.RawMessage `json:"graph_style_config"`;Job json.RawMessage `json:"job_overrides"`;Override json.RawMessage `json:"override_config"`}
 if json.Unmarshal(config,&cfg)!=nil{return nil,errors.New("invalid frozen plan config")}
 raw,e:=EffectiveApplyV4Style(cfg.Style,cfg.Job);if e==nil{raw,e=EffectiveApplyV4Style(raw,cfg.Override)};if len(cfg.Graph)>0&&string(cfg.Graph)!="null"{raw=cfg.Graph};return raw,e
}

func applyCompatiblePlan(frozen applyFrozenPlan,count int)([]ApplyV4Stage,string,error){
 if frozen.Version!=1&&frozen.Version!=2{return nil,"",errors.New("unknown apply plan contract version")}
 raw,e:=applyFrozenStyle(frozen.Config);if e!=nil{return nil,"",e};var style struct{Beauty,Defects struct{Enabled bool `json:"enabled"`};Extensions struct{Series struct{Enabled bool `json:"enabled"`} `json:"series"`} `json:"extensions"`};if json.Unmarshal(raw,&style)!=nil{return nil,"",errors.New("invalid frozen style")}
 has:=func(phase string)bool{for _,s:=range frozen.Stages{if s.Phase==phase{return true}};return false}
 if frozen.Version==1 {
  if applyStyleCullingEnabled(raw)&&!has("cull_review"){return nil,"APPLY_PLAN_RESTART_REQUIRED:CULLING_BARRIER_MISSING",nil}
  if style.Extensions.Series.Enabled&&count>128&&!has("series_pack"){return nil,"APPLY_PLAN_RESTART_REQUIRED:SERIES_DIRECT_INPUT_LIMIT",nil}
 }
 if len(frozen.Stages)>0{return frozen.Stages,"",nil}
 stages,e:=CompileApplyV4Plan(raw,count);if e!=nil{return nil,"",e}
 stages,e=editorPrecomposeStages(frozen.Config,stages);if e!=nil{return nil,"",e}
 if frozen.Version==1{
  legacy:=[]ApplyV4Stage{};for _,stage:=range stages{if stage.Phase=="series_pack"||strings.HasPrefix(stage.Phase,"cull_"){continue};if !style.Beauty.Enabled&&!style.Defects.Enabled&&(strings.HasPrefix(stage.Phase,"face_")||stage.Phase=="face_skin"){continue};legacy=append(legacy,stage)};stages=legacy
 }
 return stages,"",nil
}

func(s ApplyStore) loadFrozenPlan(ctx context.Context,l *ApplyLease)error{
 var raw json.RawMessage;if e:=s.DB.QueryRow(ctx,`SELECT photo.apply_v4_frozen_plan($1,$2,$3)`,l.TenantID,l.RunID,l.Token).Scan(&raw);e!=nil{return e};var frozen applyFrozenPlan;if json.Unmarshal(raw,&frozen)!=nil{return errors.New("frozen apply plan unavailable")}
 plan,diagnostic,e:=applyCompatiblePlan(frozen,l.ItemCount);if e!=nil{return e}
 if diagnostic!=""{var ok bool;if e=s.DB.QueryRow(ctx,`SELECT photo.request_apply_plan_restart($1,$2,$3,$4)`,l.TenantID,l.RunID,l.Token,diagnostic).Scan(&ok);e!=nil{return e};if !ok{return errors.New("apply plan compatibility cancellation rejected")};l.PlanRestartRequested=true;return nil}
 l.FrozenPlan=plan;l.PlanContractVersion=frozen.Version;return nil
}

// Saved stages, rather than the rollout version alone, determine series fan-in.
func applyUsesDirectSeries(l ApplyLease) bool {
 if l.GraphVersion != 4 { return true }
 for _, stage := range l.FrozenPlan { if stage.Phase == "series_pack" { return false } }
 return l.PlanContractVersion == 1
}
