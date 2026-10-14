package delivery

import (
 "context"
 "errors"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5"
 "photoeditor/backend/internal/v2input"
)

type DelegatedDirectProducerPort interface{
 ResolveDelegatedDirectImageProducer(context.Context,v2input.Ref)(uuid.UUID,error)
}
type DelegatedAdoptedProducerPort interface{
 ResolveDelegatedAdoptedImageProducer(context.Context,v2input.Ref)(uuid.UUID,error)
}

type DelegatedImageProducerRouter struct{
 Direct DelegatedDirectProducerPort
 Adopted DelegatedAdoptedProducerPort
}

// A genuine no-row from direct output lookup alone permits the adopted-child
// path. Integrity, scope and database errors cannot be masked by fallback.
func(s DelegatedImageProducerRouter)ResolveDelegatedImageProducer(ctx context.Context,
 image v2input.Ref)(uuid.UUID,error){
 if err:=ctx.Err();err!=nil{return uuid.Nil,err}
 if s.Direct==nil||s.Adopted==nil{return uuid.Nil,v2input.ErrInvalid}
 id,err:=s.Direct.ResolveDelegatedDirectImageProducer(ctx,image)
 if !errors.Is(err,pgx.ErrNoRows){return id,err}
 return s.Adopted.ResolveDelegatedAdoptedImageProducer(ctx,image)
}
