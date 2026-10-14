package v2optical
import("context";"testing";"photoeditor/backend/internal/v2input")
func TestPerspectiveReplacementDetectionSourceRequiresPhysicalPreparation(t *testing.T){
 var step PreparedMappedReplacementStep
 if _,err:=step.PerspectiveDetectionSource(context.Background());err!=v2input.ErrInvalid{t.Fatal("unproved source accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if _,err:=step.PerspectiveDetectionSource(ctx);err!=context.Canceled{t.Fatal("cancelled source preparation accepted",err)}
}
