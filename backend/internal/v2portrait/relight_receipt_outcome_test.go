package v2portrait

import (
 "errors"
 "testing"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/v2input"
)
func TestStrictRelightReceiptOutcome(t *testing.T) {
 for _,tc:=range []struct{raw string;expected error;code string}{
  {`{"outcome":"absent"}`,v2input.ErrExpired,""},
  {`{"outcome":"conflict"}`,v2input.ErrConflict,""},
  {`{"outcome":"denied"}`,nil,"NOT_FOUND"},
  {`null`,v2input.ErrInvalid,""},
  {`{"outcome":"unknown"}`,v2input.ErrInvalid,""},
  {`{"outcome":"absent","receipt":{}}`,v2input.ErrInvalid,""},
  {`{"outcome":"receipt","receipt":null}`,v2input.ErrInvalid,""},
  {`{"outcome":"receipt","receipt":[],"extra":true}`,v2input.ErrInvalid,""},
 } {t.Run(tc.raw,func(t *testing.T){
  body,err:=decodeRelightReceiptOutcome([]byte(tc.raw))
  if len(body)!=0 || err==nil {t.Fatal("unsafe receipt outcome",string(body),err)}
  if tc.expected!=nil && !errors.Is(err,tc.expected) {t.Fatal(err)}
  if tc.code!="" && domain.AsError(err).Code!=tc.code {t.Fatal(err)}
 })}
}
func TestStrictRelightReceiptOutcomeOwnsCanonicalReceipt(t *testing.T) {
 raw:=[]byte(`{"outcome":"receipt","receipt":{"state":"queued","run_id":"r"}}`)
 body,err:=decodeRelightReceiptOutcome(raw)
 if err!=nil || string(body)!=`{"run_id":"r","state":"queued"}` {t.Fatal(string(body),err)}
 for i:=range raw {raw[i]='x'}
 if string(body)!=`{"run_id":"r","state":"queued"}` {t.Fatal("borrowed receipt bytes")}
}
