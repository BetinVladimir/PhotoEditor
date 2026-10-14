package v2optical

import (
 "context"
 "testing"
)
func TestAutomaticPerspectiveDetectionSelectionCancellation(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 var step PreparedMappedReplacementStep
 if selected,err:=step.AutomaticPerspectiveDetection(ctx);selected||err!=context.Canceled{t.Fatal(selected,err)}
 if selected,err:=step.AutomaticPerspectiveDetection(context.Background());selected||err!=nil{t.Fatal("empty unrelated step selected",selected,err)}
}
