/** Bounded readonly request. The parent lifetime remains valid for exact journal
 * recovery; the child transport is cancelled and a late result is ignored. */
export function readDeadline<T>(parent:AbortSignal|undefined,read:(signal:AbortSignal)=>Promise<T>,milliseconds=8000):Promise<T>{
 if(!Number.isSafeInteger(milliseconds)||milliseconds<1)throw new Error('READ_DEADLINE_INVALID');
 return new Promise<T>((resolve,reject)=>{
  const controller=new AbortController();let settled=false;let timeout:ReturnType<typeof setTimeout>|undefined;
  const finish=(success:boolean,result:unknown)=>{
   if(settled)return;settled=true;if(timeout!==undefined)clearTimeout(timeout);parent?.removeEventListener('abort',abort);
   if(success)resolve(result as T);else reject(result);
  };
  const abort=()=>{const error=parent?.reason??Object.assign(new Error('Read cancelled'),{name:'AbortError'});controller.abort(error);finish(false,error);};
  if(parent?.aborted){abort();return;}
  parent?.addEventListener('abort',abort,{once:true});
  timeout=setTimeout(()=>{const error=Object.assign(new Error('Read timed out'),{name:'TimeoutError'});controller.abort(error);finish(false,error);},milliseconds);
  try{Promise.resolve(read(controller.signal)).then(value=>finish(true,value),error=>finish(false,error));}catch(error){finish(false,error);}
 });
}
