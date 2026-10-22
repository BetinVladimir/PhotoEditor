/** Preserve pending journals on scope loss: no server rejection was observed. */
export async function admitRunMutation<T>(current:()=>boolean,signal:AbortSignal|undefined,send:()=>Promise<T>):Promise<T>{
 const active=()=>{if(signal?.aborted||!current())throw new Error('RUN_MUTATION_SCOPE_CHANGED');};
 active();const result=await send();active();return result;
}
