/** One collection lifetime; bounded reads, duplicate suppression, abort fencing. */
export type CollectionPreviewQueue<T> = ((item:T)=>Promise<void>) & {invalidate:(id:string)=>void};
type RetryOptions<T> = {shouldRetry?:(item:T)=>boolean};
export function collectionPreviewQueue<T extends {id:string}>(signal:AbortSignal,load:(item:T)=>Promise<void|boolean>,concurrency=4,options:RetryOptions<T>={}):CollectionPreviewQueue<T>{
 if(!Number.isInteger(concurrency)||concurrency<1)throw new Error('PREVIEW_CONCURRENCY_INVALID');
 type Entry={item:T;promise:Promise<void>;resolve:()=>void;reject:(error:unknown)=>void;running:boolean;invalidated:boolean;retries:number;timer?:ReturnType<typeof setTimeout>};
 const known=new Map<string,Entry>(),pending:Entry[]=[];let active=0;
 function forget(entry:Entry){if(entry.timer!==undefined)clearTimeout(entry.timer);if(known.get(entry.item.id)===entry)known.delete(entry.item.id);}
 function retry(entry:Entry){
  if(signal.aborted||entry.invalidated||options.shouldRetry?.(entry.item)===false){forget(entry);return;}
  const delay=Math.min(30000,1000*2**Math.min(entry.retries++,5));
  entry.timer=setTimeout(()=>{
   entry.timer=undefined;
   if(signal.aborted||entry.invalidated||options.shouldRetry?.(entry.item)===false){forget(entry);return;}
   pending.push(entry);pump();
  },delay);
 }
 function pump(){
  while(!signal.aborted&&active<concurrency&&pending.length){
   const job=pending.shift()!;active++;job.running=true;
   void Promise.resolve().then(()=>{
    if(signal.aborted||job.invalidated)return;
    if(job.retries>0&&options.shouldRetry?.(job.item)===false){forget(job);return;}
    return load(job.item);
   }).then(result=>{if(result===false)retry(job);job.resolve();},error=>{forget(job);job.reject(error);}).finally(()=>{
    job.running=false;active--;if(job.invalidated)forget(job);pump();
   });
  }
 }
 signal.addEventListener('abort',()=>{
  for(const entry of known.values())if(entry.timer!==undefined)clearTimeout(entry.timer);
  known.clear();for(const job of pending.splice(0))job.resolve();
 },{once:true});
 const queue=((item:T):Promise<void>=>{
  if(signal.aborted)return Promise.resolve();
  const existing=known.get(item.id);if(existing)return existing.promise;
  let resolve!:()=>void,reject!:(error:unknown)=>void;
  const promise=new Promise<void>((done,fail)=>{resolve=done;reject=fail;});
  const entry:Entry={item,promise,resolve,reject,running:false,invalidated:false,retries:0};
  known.set(item.id,entry);pending.push(entry);pump();return promise;
 }) as CollectionPreviewQueue<T>;
 queue.invalidate=id=>{const entry=known.get(id);if(entry){entry.invalidated=true;if(!entry.running)forget(entry);}};
 return queue;
}
