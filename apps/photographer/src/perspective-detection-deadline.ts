// Owns timer/listener lifetime and settles even if transport ignores abort.
// The transport promise remains observed; late errors cannot become unhandled.
export async function withPerspectiveDetectionDeadline<T>(parent:AbortSignal,milliseconds:number,work:(signal:AbortSignal)=>Promise<T>):Promise<T>{
 if(!Number.isSafeInteger(milliseconds)||milliseconds<1||milliseconds>30_000)throw new Error('PERSPECTIVE_DETECTION_TIMEOUT_INVALID');
 if(parent.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');
 const controller=new AbortController();let timer:ReturnType<typeof setTimeout>|undefined;
 let rejectAbort!:(error:Error)=>void;
 const aborted=new Promise<never>((_resolve,reject)=>{rejectAbort=reject;});
 const cancel=()=>{controller.abort();rejectAbort(new Error('PERSPECTIVE_DETECTION_CANCELLED'));};
 parent.addEventListener('abort',cancel,{once:true});
 timer=setTimeout(()=>{controller.abort();rejectAbort(new Error('PERSPECTIVE_DETECTION_TIMEOUT'));},milliseconds);
 try{
  if(parent.aborted){cancel();return await aborted;}
  return await Promise.race([Promise.resolve().then(()=>{if(controller.signal.aborted)throw new Error('PERSPECTIVE_DETECTION_CANCELLED');return work(controller.signal);}),aborted]);
 }finally{if(timer!==undefined)clearTimeout(timer);parent.removeEventListener('abort',cancel);}
}
