import {operationAvailable} from './operation-availability';
import type {Capabilities} from './catalog-types';
import type {MultiOperation} from './multi-intent';
export type MultiAdmissionAPI={capabilities:(signal?:AbortSignal)=>Promise<Capabilities>;multiCapabilities:(operation:MultiOperation,project:string,signal?:AbortSignal)=>Promise<unknown>};
/** Fresh operation configuration does not prove that a compatible worker exists. */
export async function authorizeMultiOperation(api:MultiAdmissionAPI,operation:MultiOperation,project:string,current:()=>boolean,signal?:AbortSignal):Promise<void>{
 const active=()=>{if(signal?.aborted||!current())throw new Error('MULTI_ATTEMPT_CANCELLED');};
 active();await api.multiCapabilities(operation,project,signal);active();
 const capabilities=await api.capabilities(signal);active();
 if(!operationAvailable(capabilities,operation))throw new Error('MULTI_OPERATION_UNAVAILABLE');
}
