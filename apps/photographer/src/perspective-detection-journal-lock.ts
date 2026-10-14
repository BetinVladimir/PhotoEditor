const journals=new WeakMap<object,Map<string,Promise<void>>>();
export interface DetectionWebLocks {
 request<T>(name:string,options:{mode:'exclusive';signal:AbortSignal},callback:()=>Promise<T>):Promise<T>;
}
function browserLocks():DetectionWebLocks|undefined{
 const nav=(globalThis as unknown as {navigator?:{locks?:DetectionWebLocks}}).navigator;
 return nav?.locks&&typeof nav.locks.request==='function'?nav.locks:undefined;
}
// Web Locks coordinate same-origin tabs. Native/unavailable browsers use the
// process queue; server idempotency remains the final admission authority.
export async function withDetectionJournalLock<T>(journal:object,key:string,signal:AbortSignal,work:()=>Promise<T>,webLocks:DetectionWebLocks|undefined=browserLocks()):Promise<T>{
 if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const run=()=>localJournalLock(journal,key,signal,work);
 if(webLocks)return webLocks.request('photoeditor:'+key,{mode:'exclusive',signal},run);
 return run();
}
async function localJournalLock<T>(journal:object,key:string,signal:AbortSignal,work:()=>Promise<T>):Promise<T>{
 let locks=journals.get(journal);if(!locks){locks=new Map();journals.set(journal,locks);}
 const previous=locks.get(key)??Promise.resolve();
 let release!:()=>void;const turn=new Promise<void>(resolve=>{release=resolve;});
 const tail=previous.then(()=>turn);locks.set(key,tail);
 let rejectAbort!:(error:Error)=>void;
 const cancelled=new Promise<never>((_resolve,reject)=>{rejectAbort=reject;});
 const abort=()=>rejectAbort(new Error('PERSPECTIVE_DETECTION_CANCELLED'));
 signal.addEventListener('abort',abort,{once:true});
 try{
  if(signal.aborted)abort();
  await Promise.race([previous,cancelled]);
  if(signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
  // Once admitted, work owns cancellation. Never release a write lock early
  // while a transport/storage mutation may still be completing.
  return await work();
 }finally{
  signal.removeEventListener('abort',abort);release();
  // A cancelled waiter keeps its place until predecessors finish. Removing
  // this tail early would let a third writer overlap the first one.
  void tail.then(()=>{if(locks!.get(key)===tail)locks!.delete(key);});
 }
}
