/** Local cancellation never claims that an uncancellable storage write rolled
 * back. Each store retains exact-key/CAS serialization independently. */
export function waitEditorStorage<T>(pending:Promise<T>,signal:AbortSignal,timeoutMs=8000):Promise<T>{
 signal.throwIfAborted();
 return new Promise((resolve,reject)=>{
  const finish=(action:()=>void)=>{clearTimeout(timer);signal.removeEventListener('abort',abort);action();};
  const abort=()=>finish(()=>reject(signal.reason??new DOMException('Aborted','AbortError')));
  const timer=setTimeout(()=>finish(()=>reject(new Error('EDITOR_STORAGE_TIMEOUT'))),timeoutMs);
  signal.addEventListener('abort',abort,{once:true});
  pending.then(value=>finish(()=>resolve(value)),error=>finish(()=>reject(error)));
 });
}
