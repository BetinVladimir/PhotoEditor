package interchange
import (
 "context"
 "photoeditor/backend/internal/v2draft"
 "photoeditor/backend/internal/v2input"
)
// The base policy still owns recipe source/model/operation context.
type ExportStatsSemantics struct {Authority ExportStatsAuthority;Other ExportDependencySemantics}
func(s ExportStatsSemantics)QualifyExportDependency(ctx context.Context,entry v2input.Entry,raw []byte,hints []v2draft.LayerRef)error {
 if err:=ctx.Err();err!=nil{return err}
 if s.Other==nil{return v2input.ErrInvalid}
 if entry.Contract=="Stats20" {
  if s.Authority==nil{return v2input.ErrInvalid}
  if err:=s.Authority.QualifyExportStats(ctx,entry,append([]byte(nil),raw...));err!=nil{return err}
 }
 return s.Other.QualifyExportDependency(ctx,entry,raw,hints)
}
