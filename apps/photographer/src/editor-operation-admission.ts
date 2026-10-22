import type {Capabilities} from './catalog-types';
import {requireOperationAvailable} from './operation-availability';

// Capability is checked immediately before new transport. Once accepted, a
// deployment change must not discard the result; route replacement must.
export async function admitEditorOperation<T>(operation:string,scope:{current:()=>boolean;capabilities:()=>Capabilities|undefined},submit:()=>Promise<T>):Promise<T>{
 if(!scope.current())throw new Error('EDITOR_ROUTE_CHANGED');
 requireOperationAvailable(scope.capabilities(),operation);
 const accepted=await submit();
 if(!scope.current())throw new Error('EDITOR_ROUTE_CHANGED');
 return accepted;
}
