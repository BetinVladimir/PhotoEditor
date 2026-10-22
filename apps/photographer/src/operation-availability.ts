import type {Capabilities} from './catalog-types';

// Availability belongs to the server deployment, not to the client's OS.
export function operationAvailable(capabilities:Capabilities|undefined,operation:string):boolean{
 if(!Array.isArray(capabilities?.operations))return false;
 const matches=capabilities.operations.filter(value=>value&&value.operation===operation);
 if(matches.length!==1)return false;
 const entry=matches[0];
 const validBackends=(value:unknown):value is string[]=>Array.isArray(value)
  &&value.every(backend=>typeof backend==='string'&&['cpu','cuda','mps'].includes(backend))
  &&new Set(value).size===value.length;
 return entry.available===true&&entry.status==='ready'
  &&validBackends(entry.available_backends)&&validBackends(entry.supported_backends)
  &&entry.available_backends.length>0
  &&entry.available_backends.every(backend=>entry.supported_backends.includes(backend));
}
export function requireOperationAvailable(capabilities:Capabilities|undefined,operation:string):void{
 if(!operationAvailable(capabilities,operation))throw new Error('EDITOR_OPERATION_UNAVAILABLE');
}
