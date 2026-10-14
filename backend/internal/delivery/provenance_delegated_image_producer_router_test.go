package delivery

import (
 "context"
 "errors"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type delegatedDirectProducerFunc func(context.Context,v2input.Ref)(uuid.UUID,error)
func(f delegatedDirectProducerFunc)ResolveDelegatedDirectImageProducer(ctx context.Context,
 ref v2input.Ref)(uuid.UUID,error){return f(ctx,ref)}
type delegatedAdoptedProducerFunc func(context.Context,v2input.Ref)(uuid.UUID,error)
func(f delegatedAdoptedProducerFunc)ResolveDelegatedAdoptedImageProducer(ctx context.Context,
 ref v2input.Ref)(uuid.UUID,error){return f(ctx,ref)}

func TestDelegatedProducerRouterFallsBackOnlyOnNoRows(t *testing.T){
 expected:=uuid.New()
 integrity:=errors.New("integrity")
 for _,mode:=range []string{"direct","missing","integrity"}{
  t.Run(mode,func(t *testing.T){
   adoptedCalls:=0
   direct:=delegatedDirectProducerFunc(func(context.Context,v2input.Ref)(uuid.UUID,error){
    switch mode{
    case "direct":return expected,nil
    case "missing":return uuid.Nil,pgx.ErrNoRows
    default:return uuid.Nil,integrity
    }
   })
   adopted:=delegatedAdoptedProducerFunc(func(context.Context,v2input.Ref)(uuid.UUID,error){
    adoptedCalls++
    return expected,nil
   })
   got,err:=(DelegatedImageProducerRouter{Direct:direct,Adopted:adopted}).
    ResolveDelegatedImageProducer(context.Background(),v2input.Ref{})
   if mode=="integrity"{
    if !errors.Is(err,integrity)||adoptedCalls!=0{t.Fatal("integrity hidden by fallback")}
   }else if err!=nil||got!=expected||adoptedCalls!=map[string]int{"direct":0,"missing":1}[mode]{
    t.Fatal("producer route drift",err)
   }
  })
 }
}
