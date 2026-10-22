import {APIError} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {operationAvailable} from './operation-availability';

/** Fresh analysis requires both descriptor inference and CPU planning. */
export async function requireSessionAnalysisAvailable(api:Pick<OwnerAPI,'capabilities'>,signal:AbortSignal):Promise<void>{
 const capabilities=await api.capabilities(signal);
 if(signal.aborted){const error=new Error('Session scope cancelled');error.name='AbortError';throw error;}
 if(!operationAvailable(capabilities,'session.describe')||!operationAvailable(capabilities,'session.plan'))
  throw new APIError(503,'SESSION_OPERATION_UNAVAILABLE');
}
