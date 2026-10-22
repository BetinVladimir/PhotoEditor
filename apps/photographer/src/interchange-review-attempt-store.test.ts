import {executeInterchangeReview} from './interchange-review-controller';
import {expect,it,vi} from 'vitest';
import {InterchangeReviewAttemptStore,clearInterchangeReviewAttempts,type InterchangeReviewAttempt} from './interchange-review-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_origin:string,action:()=>Promise<T>)=>action();
function storage(){const values=new Map<string,string>();return {getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)},getAllKeys:async()=>[...values.keys()]};}
function record(kind:'decide'|'close'='decide'):InterchangeReviewAttempt{return {version:1,kind,key:'original-key',receipt:null,
 inspection:{schema_version:1,workflow_id:id(4),tenant_id:id(2),project_id:id(3),run_id:id(5),revision:'1',status:'waiting_review',stage:'review',ingest_run_id:null,collection_id:null,
 review:{report:{artifact_id:id(6),sha256:'a'.repeat(64)},proposed_count:kind==='decide'?1:0,entry_count:1,can_close:kind==='close',can_decide:kind==='decide',targets:kind==='decide'?[{entry_id:id(7),external_id:'photo',asset_version_id:id(8),state:'proposed',fields:[]}]:[]}},
 decisions:kind==='decide'?[{entry_id:id(7),decision:'approve'}]:[]};}
function store(data:ReturnType<typeof storage>,origin='origin'){return new InterchangeReviewAttemptStore(data,origin,id(1),id(2),id(3),id(4),lock);}
function accepted(kind:'decide'|'close'){return kind==='decide'?{workflow_id:id(4),revision:'2',batch_id:id(9),remaining:0,child:null}:{workflow_id:id(4),run_id:id(5),revision:'2',state:'completed',outputs:[],error_code:null};}
it.each(['decide','close'] as const)('owns and restores exact %s request across mounts',async kind=>{
 const data=storage(),first=store(data),original=record(kind);await first.write(original);
 original.inspection.revision='9';const restored=await store(data).read();expect(restored).toEqual(record(kind));
 restored!.inspection.revision='8';expect(await first.read()).toEqual(record(kind));
 await expect(first.clear('original-key')).rejects.toThrow('CONFLICT');
 await first.write({...record(kind),receipt:accepted(kind)});
 await expect(async()=>first.write(record(kind))).rejects.toThrow('CONFLICT');
 await expect(first.clear('another-key')).rejects.toThrow('CONFLICT');
 await first.clear('original-key');expect(await first.read()).toBeNull();
});
it('refuses changed intent, key, revision, report and receipt',async()=>{
 const data=storage(),journal=store(data),original=record();await journal.write(original);
 const revision=record();revision.inspection.revision='2';
 const report=record();report.inspection.review!.report.sha256='b'.repeat(64);
 const decision=record();decision.decisions[0].decision='reject';
 for(const changed of [{...record(),key:'new-key'},revision,report,decision])await expect(async()=>journal.write(changed)).rejects.toThrow('CONFLICT');
 await expect(async()=>journal.write({...record(),receipt:{...accepted('decide'),revision:'3'}})).rejects.toThrow();
 expect(await journal.read()).toEqual(record());
});
it('isolates scopes and invalidates mounted stores after logout',async()=>{
 const data=storage(),journal=store(data,'logout-origin');await journal.write(record());
 expect(()=>journal.assertScope(id(2),id(3),id(90))).toThrow('SCOPE_INVALID');
 expect(await new InterchangeReviewAttemptStore(data,'logout-origin',id(90),id(2),id(3),id(4),lock).read()).toBeNull();
 await clearInterchangeReviewAttempts(data,'logout-origin',lock);
 await expect(async()=>journal.write(record())).rejects.toThrow('EXPIRED');
 expect(await store(data,'logout-origin').read()).toBeNull();
});

it('admits exactly one of two concurrent distinct immutable intents',async()=>{
 const data=storage(),first=store(data,'concurrent-origin'),second=store(data,'concurrent-origin');
 const a=record(),b={...record(),key:'other-key',decisions:[{entry_id:id(7),decision:'reject' as const}]};
 const outcomes=await Promise.allSettled([first.write(a),second.write(b)]);
 expect(outcomes.filter(value=>value.status==='fulfilled')).toHaveLength(1);
 expect(outcomes.filter(value=>value.status==='rejected')).toHaveLength(1);
 const saved=await first.read();expect(saved).toEqual(a);
 expect(await second.read()).toEqual(a);
});
it('old acknowledgement cannot remove a newly admitted operation',async()=>{
 const data=storage(),first=store(data,'new-operation'),second=store(data,'new-operation');
 await first.write({...record(),receipt:accepted('decide')});await first.clear('original-key');
 const newer={...record(),key:'new-key'};await second.write(newer);
 await expect(first.clear('original-key')).rejects.toThrow('CONFLICT');
 await expect(async()=>first.write({...record(),receipt:accepted('decide')})).rejects.toThrow('CONFLICT');
 expect(await second.read()).toEqual(newer);
});
it('observes foreign-tab generation change before write or clear',async()=>{
 const data=storage(),journal=store(data,'foreign-generation');await journal.write(record());
 // Simulate another JS realm changing persisted logout generation, without
 // changing this realm's in-memory epoch map. Browser lock is doubled here.
 await data.setItem('photo.interchange-review-generation.v1:'+JSON.stringify('foreign-generation'),id(90));
 await expect(journal.read()).rejects.toThrow('EXPIRED');
 await expect(async()=>journal.write({...record(),receipt:accepted('decide')})).rejects.toThrow('EXPIRED');
 await expect(journal.clear('original-key')).rejects.toThrow('EXPIRED');
 const fresh=store(data,'foreign-generation');expect(await fresh.read()).toEqual(record());
});
it('logout clears only journals belonging to its origin',async()=>{
 const data=storage(),one=store(data,'logout-one'),two=store(data,'logout-two');
 await one.write(record());await two.write(record());
 await clearInterchangeReviewAttempts(data,'logout-one',lock);
 await expect(one.read()).rejects.toThrow('EXPIRED');
 expect(await two.read()).toEqual(record());expect(await store(data,'logout-one').read()).toBeNull();
});


it.each(['decide','close'] as const)('review controller retries lost %s response with original key',async kind=>{
 const data=storage(),journal=store(data),original=record(kind);
 const mutation=vi.fn().mockRejectedValueOnce(new Error('lost response')).mockResolvedValueOnce(accepted(kind));
 const api={decideInterchangeReview:mutation,closeInterchangeReview:mutation};
 const options={api,journal,tenant:id(2),project:id(3),workflow:id(4),record:original,signal:new AbortController().signal,lock};
 await expect(executeInterchangeReview(options)).rejects.toThrow('lost response');expect((await journal.read())?.key).toBe(original.key);
 await executeInterchangeReview(options);expect(await journal.read()).toBeNull();
 expect(mutation).toHaveBeenCalledTimes(2);
 expect(mutation.mock.calls.every(call=>call[kind==='decide'?3:2]===original.key)).toBe(true);
});
it('review controller reuses persisted receipt without another mutation',async()=>{
 const data=storage(),journal=store(data),original=record();await journal.write({...original,receipt:accepted('decide')});
 const api={decideInterchangeReview:vi.fn(),closeInterchangeReview:vi.fn()};
 await executeInterchangeReview({api,journal,tenant:id(2),project:id(3),workflow:id(4),record:original,signal:new AbortController().signal,lock});
 expect(api.decideInterchangeReview).not.toHaveBeenCalled();expect(await journal.read()).toBeNull();
});


it('review recovery refuses deleted attempt without recreating intent',async()=>{
 const data=storage(),journal=store(data),api={decideInterchangeReview:vi.fn(),closeInterchangeReview:vi.fn()};
 await expect(executeInterchangeReview({api,journal,tenant:id(2),project:id(3),workflow:id(4),record:record(),requireExisting:true,signal:new AbortController().signal,lock})).rejects.toThrow('CONFLICT');
 expect(api.decideInterchangeReview).not.toHaveBeenCalled();expect(await journal.read()).toBeNull();
});
it('review response cannot resurrect a journal removed during transport',async()=>{
 const data=storage(),journal=store(data);
 const api={decideInterchangeReview:vi.fn(async()=>{await data.removeItem(journal.key);return accepted('decide');}),closeInterchangeReview:vi.fn()};
 await expect(executeInterchangeReview({api,journal,tenant:id(2),project:id(3),workflow:id(4),record:record(),signal:new AbortController().signal,lock})).rejects.toThrow('CONFLICT');
 expect(await journal.read()).toBeNull();
});
it('review callbacks cannot change accepted selection or key',async()=>{
 const data=storage(),journal=store(data);
 const api={decideInterchangeReview:vi.fn(async()=>accepted('decide')),closeInterchangeReview:vi.fn()};
 await executeInterchangeReview({api,journal,tenant:id(2),project:id(3),workflow:id(4),record:record(),signal:new AbortController().signal,lock,onAttempt:owned=>{owned.key='changed';owned.decisions[0].decision='reject';}});
 expect(api.decideInterchangeReview).toHaveBeenCalledWith(id(3),record().inspection,record().decisions,'original-key',expect.any(AbortSignal));
 expect(await journal.read()).toBeNull();
});


it('cancellation after durable review receipt retries cleanup without another POST',async()=>{
 const data=storage(),journal=store(data),controller=new AbortController();
 const api={decideInterchangeReview:vi.fn(async()=>accepted('decide')),closeInterchangeReview:vi.fn()};
 const options={api,journal,tenant:id(2),project:id(3),workflow:id(4),record:record(),signal:controller.signal,lock,onAttempt:(current:InterchangeReviewAttempt)=>{if(current.receipt!==null)controller.abort();}};
 await expect(executeInterchangeReview(options)).rejects.toThrow();
 const saved=await journal.read();expect(saved?.receipt).not.toBeNull();
 await executeInterchangeReview({...options,record:saved!,requireExisting:true,signal:new AbortController().signal,onAttempt:undefined});
 expect(api.decideInterchangeReview).toHaveBeenCalledOnce();expect(await journal.read()).toBeNull();
});


it('review controller accepts normalized OwnerAPI close receipt and persists canonical HTTP receipt',async()=>{
 const data=storage(),journal=store(data),seen:InterchangeReviewAttempt[]=[];
 const api={decideInterchangeReview:vi.fn(),closeInterchangeReview:vi.fn(async()=>({workflow_id:id(4),run_id:id(5),revision:'2',state:'completed'}))};
 await executeInterchangeReview({api,journal,tenant:id(2),project:id(3),workflow:id(4),record:record('close'),signal:new AbortController().signal,lock,onAttempt:current=>{seen.push(current);}});
 expect(seen[1].receipt).toEqual(accepted('close'));expect(await journal.read()).toBeNull();
});


it('atomic review cleanup requires the exact acknowledged attempt',async()=>{
 const data=storage(),journal=store(data),ack={...record(),receipt:accepted('decide')};
 await journal.write(ack);
 const changed={...ack,inspection:{...ack.inspection,revision:'2'},receipt:{...accepted('decide'),revision:'3'}};
 await expect(journal.clear(ack.key,changed)).rejects.toThrow('CONFLICT');
 expect(await journal.read()).toEqual(ack);
 await journal.clear(ack.key,ack);expect(await journal.read()).toBeNull();
 await expect(journal.clear(ack.key,ack)).rejects.toThrow('CONFLICT');
});
