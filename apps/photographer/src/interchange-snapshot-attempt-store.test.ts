import {expect,it,vi} from 'vitest';
import {executeInterchangeSnapshot,openInterchangeSnapshot} from './interchange-snapshot-controller';
import {canonicalJSON} from '@photoeditor/api-client';
import {sha256} from '@noble/hashes/sha2.js';
import {bytesToHex} from '@noble/hashes/utils.js';
import {InterchangeSnapshotAttemptStore,clearInterchangeSnapshotAttempts} from './interchange-snapshot-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const hash=(v:unknown)=>bytesToHex(sha256(new TextEncoder().encode(canonicalJSON(v))));
const request=()=>({items:[{ordinal:0,binding:{asset_version_id:id(3),source:{artifact_id:id(4),sha256:'a'.repeat(64)},canvas_hash:'b'.repeat(64)},working_image:{artifact_id:id(4),sha256:'a'.repeat(64)},recipe:{artifact_id:id(5),sha256:'c'.repeat(64)},captured_at:null,camera_hash:null}]});
const pending=()=>({version:1 as const,key:'original',body:request(),receipt:null});
function receipt(){const unsigned={schema_version:1,snapshot_id:id(6),items:request().items},digest=hash(unsigned);return {id:id(6),digest,item_count:1,artifact:{artifact_id:id(7),sha256:hash({...unsigned,snapshot_digest:digest})}};}
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
const store=(s:ReturnType<typeof storage>,origin='https://snapshot.test',project=id(2))=>new InterchangeSnapshotAttemptStore(s,origin,id(9),id(1),project,request(),lock);
it('restores exact request/key, owns bytes and only clears an acknowledged attempt',async()=>{
 const s=storage(),first=store(s);await first.write(pending());await expect(first.clear('original')).rejects.toThrow();
 const restored=store(s),saved=await restored.read();expect(saved).toEqual(pending());saved!.body.items[0].recipe.sha256='d'.repeat(64);expect(await restored.read()).toEqual(pending());
 await restored.write({...pending(),receipt:receipt()});await expect(restored.write(pending())).rejects.toThrow();await expect(restored.clear('foreign')).rejects.toThrow();await restored.clear('original');expect(await restored.read()).toBeNull();
});
it('pins project/source/recipe independently and owns constructor input',()=>{
 const s=storage(),body=request(),journal=new InterchangeSnapshotAttemptStore(s,'https://snapshot.test',id(9),id(1),id(2),body,lock);body.items[0].recipe.sha256='d'.repeat(64);
 journal.assertScope(id(1),id(2),request());expect(()=>journal.assertScope(id(1),id(8),request())).toThrow();expect(()=>journal.assertScope(id(1),id(2),body)).toThrow();
 expect(journal.key).not.toBe(store(s,'https://snapshot.test',id(8)).key);
});
it('fences mounted journals after logout and isolates other origins',async()=>{
 const s=storage(),first=store(s),other=store(s,'https://other.test');await first.write(pending());await other.write(pending());
 await clearInterchangeSnapshotAttempts(s,'https://snapshot.test',lock);await expect(first.read()).rejects.toThrow();await expect(first.write(pending())).rejects.toThrow();
 expect(await store(s).read()).toBeNull();expect(await other.read()).toEqual(pending());
});
it('rejects substituted receipt and a replacement idempotency key',async()=>{
 const journal=store(storage());await journal.write(pending());await expect(journal.write({...pending(),key:'replacement'})).rejects.toThrow();
 expect(()=>journal.write({...pending(),receipt:{...receipt(),digest:'d'.repeat(64)}})).toThrow();
});

it('serializes snapshot preparation and reuses the durable receipt',async()=>{
 const journal=store(storage()),prepareInterchangeSnapshot=vi.fn(async()=>receipt()),newKey=vi.fn(()=> 'original');
 const options={api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),signal:new AbortController().signal,newKey,lock};
 const [first,second]=await Promise.all([executeInterchangeSnapshot(options),executeInterchangeSnapshot(options)]);
 expect(first).toEqual({...pending(),receipt:receipt()});expect(second).toEqual(first);
 expect(newKey).toHaveBeenCalledTimes(1);expect(prepareInterchangeSnapshot).toHaveBeenCalledTimes(1);
});
it('retains snapshot key after lost response and refuses deleted retry',async()=>{
 const s=storage(),journal=store(s),prepareInterchangeSnapshot=vi.fn().mockRejectedValueOnce(new Error('lost')).mockResolvedValue(receipt());
 const options={api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),signal:new AbortController().signal,newKey:()=> 'original',lock};
 await expect(executeInterchangeSnapshot(options)).rejects.toThrow('lost');expect(await journal.read()).toEqual(pending());
 await executeInterchangeSnapshot({...options,expectedAttemptKey:'original'});
 expect(prepareInterchangeSnapshot.mock.calls.map(call=>call[2])).toEqual(['original','original']);
 await s.removeItem(journal.key);await expect(executeInterchangeSnapshot({...options,expectedAttemptKey:'original'})).rejects.toThrow('CONFLICT');
 expect(prepareInterchangeSnapshot).toHaveBeenCalledTimes(2);
});
it('rejects late acknowledgement and stale handoff after durable removal',async()=>{
 const s=storage(),journal=store(s),prepareInterchangeSnapshot=vi.fn(async()=>{await s.removeItem(journal.key);return receipt()});
 const options={api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),signal:new AbortController().signal,newKey:()=> 'original',lock};
 await expect(executeInterchangeSnapshot(options)).rejects.toThrow('CONFLICT');expect(await journal.read()).toBeNull();
 const expected={...pending(),receipt:receipt()};await journal.write(expected);
 const onPrepared=vi.fn(value=>{value.artifact.sha256='d'.repeat(64)});
 const handoff={journal,tenant:id(1),project:id(2),request:request(),expected,signal:options.signal,onPrepared,lock};
 await openInterchangeSnapshot(handoff);expect(await journal.read()).toEqual(expected);
 await s.removeItem(journal.key);await expect(openInterchangeSnapshot(handoff)).rejects.toThrow('CONFLICT');expect(onPrepared).toHaveBeenCalledTimes(1);
});

it('does not allocate a snapshot key or POST for an already cancelled request',async()=>{
 const journal=store(storage()),signal=new AbortController();signal.abort();
 const prepareInterchangeSnapshot=vi.fn(),newKey=vi.fn(()=> 'original');
 await expect(executeInterchangeSnapshot({api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),signal:signal.signal,newKey,lock})).rejects.toThrow();
 expect(newKey).not.toHaveBeenCalled();expect(prepareInterchangeSnapshot).not.toHaveBeenCalled();expect(await journal.read()).toBeNull();
});
it('keeps snapshot request and receipt owned across callbacks',async()=>{
 const journal=store(storage()),prepareInterchangeSnapshot=vi.fn(async(_project:string,body:ReturnType<typeof request>)=>{expect(body).toEqual(request());return receipt()});
 const options={api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),signal:new AbortController().signal,newKey:()=> 'original',lock};
 const accepted=await executeInterchangeSnapshot({...options,onAttempt:attempt=>{attempt.body.items[0].recipe.sha256='d'.repeat(64);if(attempt.receipt)attempt.receipt.artifact.sha256='e'.repeat(64)}});
 expect(accepted).toEqual({...pending(),receipt:receipt()});expect(await journal.read()).toEqual(accepted);
 await executeInterchangeSnapshot({...options,expectedAttemptKey:'original',onAttempt:attempt=>{attempt.receipt!.artifact.sha256='f'.repeat(64)}});
 expect(prepareInterchangeSnapshot).toHaveBeenCalledTimes(1);expect(await journal.read()).toEqual(accepted);
});

it('releases snapshot execution after abort even when transport ignores cancellation',async()=>{
 const journal=store(storage()),firstSignal=new AbortController();let release!:(value:ReturnType<typeof receipt>)=>void;
 const prepareInterchangeSnapshot=vi.fn().mockImplementationOnce(()=>new Promise(resolve=>{release=resolve})).mockResolvedValueOnce(receipt());
 const base={api:{prepareInterchangeSnapshot},journal,tenant:id(1),project:id(2),request:request(),newKey:()=> 'original',lock};
 const first=executeInterchangeSnapshot({...base,signal:firstSignal.signal});
 for(let n=0;n<20&&prepareInterchangeSnapshot.mock.calls.length===0;n++)await new Promise(resolve=>setTimeout(resolve,0));
 expect(prepareInterchangeSnapshot).toHaveBeenCalledTimes(1);firstSignal.abort();await expect(first).rejects.toThrow();
 expect(await journal.read()).toEqual(pending());
 const second=await executeInterchangeSnapshot({...base,signal:new AbortController().signal,expectedAttemptKey:'original'});
 expect(second.receipt).toEqual(receipt());expect(prepareInterchangeSnapshot).toHaveBeenCalledTimes(2);
 release(receipt());await Promise.resolve();expect(await journal.read()).toEqual(second);
});
