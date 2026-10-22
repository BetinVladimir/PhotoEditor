package workflow

import (
 "context"
 "errors"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/masknpy"
 "photoeditor/backend/internal/objectstore"
 "photoeditor/backend/internal/v2input"
)
// deliveryPhysicalSourceFailure is emitted only after a SQL-scoped exact object
// descriptor was obtained. Generic contract/ACL/lease errors are not evidence
// of missing physical bytes, and never enter the terminal source path.
type deliveryPhysicalSourceFailure struct{Ref v2input.Ref;Version,Code string;Cause error;Slot int;Request string;Watermark bool}
func(e *deliveryPhysicalSourceFailure)Error()string{return e.Code}
func(e *deliveryPhysicalSourceFailure)Unwrap()error{return e.Cause}
func physicalDeliveryFailure(ref v2input.Ref,version string,err error)error{
 if err==nil{return nil};code:=""
 switch{case errors.Is(err,objectstore.ErrMissing):code="SOURCE_OBJECT_MISSING";case errors.Is(err,objectstore.ErrIntegrity):code="SOURCE_OBJECT_INTEGRITY";case errors.Is(err,masknpy.ErrInvalid):code="SOURCE_NPY_INVALID"}
 if code==""{return err};return &deliveryPhysicalSourceFailure{Ref:ref,Version:version,Code:code,Cause:err}
}
type deliveryPhysicalSourceFinalizer interface{FailPhysicalSource(context.Context,delivery.ExpansionLease,v2input.Ref,string,string,int,string,bool)(bool,error)}

func bindDeliveryPhysicalFailure(target *error,slot int,request string,watermark bool){var physical *deliveryPhysicalSourceFailure;if errors.As(*target,&physical){owned:=*physical;owned.Slot=slot;owned.Request=request;owned.Watermark=watermark;*target=&owned}}

func bindDeliveryCanonicalFailure(target *error,slot int){var physical *deliveryPhysicalSourceFailure;if errors.As(*target,&physical)&&physical.Request==""{bindDeliveryPhysicalFailure(target,slot,"{}",false)}}
