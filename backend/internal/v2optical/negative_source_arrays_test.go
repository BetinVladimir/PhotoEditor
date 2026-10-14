package v2optical

import (
 "bytes"
 "context"
 "math"
 "testing"
)

func TestNegativeSourceArrayPhysicalContract(t *testing.T) {
 for _, sample := range []struct { channels int; values []float32; valid bool }{
  {3, []float32{-0.1,0.5,2},true},
  {2, []float32{-20,200},true},
  {0, []float32{0.5},true},
  {0, []float32{2},false},
  {3, []float32{0,float32(math.NaN()),1},false},
  {3, []float32{0,1},false},
 } {
  spec, object, raw := perspectiveArrayFixture(t,sample.channels,sample.values)
  err := VerifyNegativeSourceArray(context.Background(),spec,object,bytes.NewReader(raw))
  if (err==nil)!=sample.valid { t.Fatalf("channels %d: %v",sample.channels,err) }
 }
}

func TestNegativeSourceArrayCancellation(t *testing.T) {
 spec, object, raw := perspectiveArrayFixture(t,3,[]float32{0,1,2})
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 if err:=VerifyNegativeSourceArray(ctx,spec,object,bytes.NewReader(raw));err!=context.Canceled { t.Fatalf("got %v",err) }
}
