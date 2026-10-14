import {APIError} from '@photoeditor/api-client';

// A saved attempt may be removed only when the server explicitly says the
// transaction was rejected. Transport errors and generic HTTP failures can
// represent a committed mutation whose response was lost.
export function runMutationDefinitelyRejected(action:'retry'|'cancel',error:unknown):boolean{
 if(!(error instanceof APIError))return false;
 if(error.code==='VALIDATION_FAILED'&&error.status===422)return true;
 if(error.status!==409)return false;
 return action==='retry'
  ?error.code==='REVISION_CONFLICT'||error.code==='RUN_NOT_RETRYABLE'
  :error.code==='RUN_NOT_CANCELLABLE'||error.code==='RUN_STATE_INVALID';
}
