import{describe,expect,it,vi}from"vitest";import{UploadQueue,type MultipartAPI,type UploadPersistence,type UploadRecord,type UploadSource}from"./upload";
const source=(sha='a'.repeat(64)):UploadSource=>({name:'x.jpg',size:9,lastModified:7,sha256:sha,read:async(a,b)=>new Uint8Array(b-a)});
describe('UploadQueue',()=>{it('uploads in bounded two-part batches and persists no signed URLs',async()=>{let stored:UploadRecord[]=[];const signed:number[][]=[];const persistence:UploadPersistence={load:async()=>stored,save:async x=>{stored=structuredClone(x)}};const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:4,parts_total:3}),sign:async(_u,p)=>{signed.push(p);return p.map(part_no=>({part_no,url:`secret:${part_no}`}))},put:async u=>`etag-${u.split(':')[1]}`,complete:async()=>{},abort:async()=>{}};const q=new UploadQueue(api,persistence);const r=await q.enqueue(source(),{tenant:'t',projectId:'p'});for(let i=0;i<30&&q.records[0].state!=='complete';i++)await new Promise(x=>setTimeout(x,1));expect(q.records[0].state).toBe('complete');expect(signed).toEqual([[1,2],[3]]);expect(JSON.stringify(stored)).not.toContain('secret:');expect(r.parts.map(x=>x.part_no)).toEqual([1,2,3])});it('rejects a different file on resume',async()=>{let stored:UploadRecord[]=[{localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),parts:[],state:'uploading'}];const persistence:UploadPersistence={load:async()=>stored,save:async x=>{stored=x}};const api={} as MultipartAPI;const q=new UploadQueue(api,persistence);await q.restore();await expect(q.resume('l',source('b'.repeat(64)))).rejects.toThrow('UPLOAD_FILE_MISMATCH');expect(q.records[0].state).toBe('paused')})});

describe('RAW upload persistence',()=>{
 it('owns and persists the selected policy before Begin and restores it unchanged',async()=>{
  let stored:UploadRecord[]=[];let begun:UploadRecord|undefined;
  const persistence:UploadPersistence={load:async()=>stored,save:async value=>{stored=structuredClone(value)}};
  const api={begin:async(record:UploadRecord)=>{begun=structuredClone(record);throw new Error('fixture offline')}} as unknown as MultipartAPI;
  const policy={schema_version:1 as const,demosaic:'ahd' as const,wb_source:'camera' as const,exposure_ev:1.5,highlight_policy:'blend' as const,decoder_version:'rawpy-0.27.1/libraw-0.21.4'};
  const queue=new UploadQueue(api,persistence);
  const record=await queue.enqueue({...source(),name:'frame.nef'},{tenant:'t',projectId:'p',rawDevelopment:policy});
  policy.exposure_ev=2;
  expect(record.rawDevelopment?.exposure_ev).toBe(1.5);
  for(let i=0;i<20&&!begun;i++)await Promise.resolve();
  expect(begun?.rawDevelopment?.exposure_ev).toBe(1.5);
  const restored=new UploadQueue(api,persistence);await restored.restore();
  expect(restored.records[0].rawDevelopment).toEqual(record.rawDevelopment);
  expect(restored.records[0].state).toBe('paused');
 });
 it('refuses malformed persisted RAW policy before activating records',async()=>{
  const stored=[{localId:'l',tenant:'t',projectId:'p',name:'frame.nef',size:9,lastModified:7,sha256:'a'.repeat(64),parts:[],state:'paused',rawDevelopment:{}}] as unknown as UploadRecord[];
  const persistence:UploadPersistence={load:async()=>stored,save:async()=>{throw new Error('must not save')}};
  const queue=new UploadQueue({} as MultipartAPI,persistence);
  await expect(queue.restore()).rejects.toThrow('RAW_DEVELOPMENT_INVALID');
  expect(queue.records).toEqual([]);
 });
});

describe('upload queue local shutdown',()=>{
 it.each(['begin','sign','read','put','verify','complete'])('fences a late %s response after close',async phase=>{
  let release!:()=>void,reached!:()=>void;
  const gate=new Promise<void>(r=>{release=r}),entered=new Promise<void>(r=>{reached=r});
  const wait=async(name:string)=>{if(name===phase){reached();await gate}};
  let stored:UploadRecord[]=[];
  const save=vi.fn(async(value:UploadRecord[])=>{stored=structuredClone(value)});
  const persistence:UploadPersistence={load:async()=>stored,save};
  const api:MultipartAPI={begin:vi.fn(async()=>{await wait('begin');return{id:'u',part_bytes:9,parts_total:1}}),sign:vi.fn(async()=>{await wait('sign');return[{part_no:1,url:'fixture:part'}]}),put:vi.fn(async()=>{await wait('put');return'etag'}),complete:vi.fn(async()=>{await wait('complete')}),abort:vi.fn(async()=>{})};
  const file={...source(),verify:async()=>{await wait('verify');return 'a'.repeat(64)},read:vi.fn(async()=>{await wait('read');return new Uint8Array(9)})};
  const queue=new UploadQueue(api,persistence);await queue.enqueue(file,{tenant:'t',projectId:'p'});await entered;
  const writes=save.mock.calls.length;queue.close();queue.close();release();
  for(let i=0;i<20;i++)await Promise.resolve();
  expect(queue.records[0].state).toBe('paused');expect(save).toHaveBeenCalledTimes(writes);expect(api.abort).not.toHaveBeenCalled();
  if(phase==='begin'){expect(api.sign).not.toHaveBeenCalled();expect(queue.records[0].uploadId).toBeUndefined()}
  if(phase==='begin'||phase==='sign'){expect(file.read).not.toHaveBeenCalled()}
  if(['begin','sign','read'].includes(phase))expect(api.put).not.toHaveBeenCalled();
  if(phase!=='complete')expect(api.complete).not.toHaveBeenCalled();
  await expect(queue.enqueue(source(),{tenant:'t',projectId:'p'})).rejects.toThrow('UPLOAD_QUEUE_CLOSED');
 });
 it('does not install a delayed restored snapshot after close',async()=>{
  let resolve!:(rows:UploadRecord[])=>void;const load=new Promise<UploadRecord[]>(r=>{resolve=r});const save=vi.fn(async()=>{});
  const queue=new UploadQueue({} as MultipartAPI,{load:()=>load,save});const restoring=queue.restore();queue.close();resolve([]);
  await expect(restoring).rejects.toThrow('UPLOAD_QUEUE_CLOSED');expect(save).not.toHaveBeenCalled();expect(queue.records).toEqual([]);
 });
});

describe('multipart response admission',()=>{
 it.each(['zero bytes','fraction bytes','too large bytes','fraction total','wrong total','too many parts','duplicate parts','foreign part','empty URL','invalid etag'])('refuses %s before completion',async mode=>{
  const begun={id:'u',part_bytes:4,parts_total:3};
  if(mode==='zero bytes')begun.part_bytes=0;if(mode==='fraction bytes')begun.part_bytes=1.5;if(mode==='too large bytes')begun.part_bytes=16777217;if(mode==='fraction total')begun.parts_total=3.5;if(mode==='wrong total')begun.parts_total=2;if(mode==='too many parts'){begun.part_bytes=1;begun.parts_total=10001}
  const read=vi.fn(async(a:number,b:number)=>new Uint8Array(b-a));
  const api:MultipartAPI={begin:vi.fn(async()=>begun),sign:vi.fn(async(_id,parts)=>{const links=parts.map((part_no:number)=>({part_no,url:'fixture:part'}));if(mode==='duplicate parts')links[1].part_no=links[0].part_no;if(mode==='foreign part')links[0].part_no=3;if(mode==='empty URL')links[0].url='';return links}),put:vi.fn(async()=>mode==='invalid etag'?'bad\nvalue':'etag'),complete:vi.fn(async()=>{}),abort:vi.fn(async()=>{})};
  const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});
  const failed=new Promise<void>(resolve=>queue.subscribe(()=>{if(queue.records[0]?.state==='error')resolve()}));
  await queue.enqueue({...source(),read},{tenant:'t',projectId:'p'});await failed;
  expect(api.complete).not.toHaveBeenCalled();
  if(!['duplicate parts','foreign part','empty URL','invalid etag'].includes(mode))expect(api.sign).not.toHaveBeenCalled();
  if(mode==='duplicate parts'||mode==='foreign part'||mode==='empty URL'){expect(read).not.toHaveBeenCalled();expect(api.put).not.toHaveBeenCalled()}
  queue.close();
 });
});

describe('upload remote recovery',()=>{
 it.each(['completing','verifying','verified','rejected','expired','aborted'] as const)('restores %s without PUT or Complete replay',async state=>{
  const record:UploadRecord={localId:'restore',tenant:'t',projectId:'p',uploadId:'u',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),partBytes:4,partsTotal:3,parts:[{part_no:1,etag:'one'},{part_no:2,etag:'two'},{part_no:3,etag:'three'}],state:'error'};
  const api:MultipartAPI={status:vi.fn(async()=>({id:'u',part_bytes:4,parts_total:3,state})),begin:vi.fn(),sign:vi.fn(),put:vi.fn(),complete:vi.fn(),abort:vi.fn()};
  const queue=new UploadQueue(api,{load:async()=>[record],save:async()=>{}});await queue.restore();
  const expected=state==='verified'?'complete':state==='aborted'?'aborted':['completing','verifying'].includes(state)?'completing':'error';
  const settled=new Promise<void>(resolve=>queue.subscribe(()=>{if(queue.records[0].state===expected)resolve()}));
  await queue.resume('restore',source());await settled;
  expect(api.sign).not.toHaveBeenCalled();expect(api.put).not.toHaveBeenCalled();expect(api.complete).not.toHaveBeenCalled();
  if(state==='completing'||state==='verifying'){api.status=vi.fn(async()=>({id:'u',part_bytes:4,parts_total:3,state:'verified' as const}));await vi.waitFor(async()=>{await queue.refresh('restore');expect(queue.records[0].state).toBe('complete')})}
  queue.close();
 });
 it('refuses substituted current upload identity before signing',async()=>{
  const api:MultipartAPI={begin:vi.fn(async()=>({id:'u',part_bytes:9,parts_total:1})),status:vi.fn(async()=>({id:'foreign',part_bytes:9,parts_total:1,state:'uploading' as const})),sign:vi.fn(),put:vi.fn(),complete:vi.fn(),abort:vi.fn()};
  const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});const failed=new Promise<void>(resolve=>queue.subscribe(()=>{if(queue.records[0]?.state==='error')resolve()}));
  await queue.enqueue(source(),{tenant:'t',projectId:'p'});await failed;expect(api.sign).not.toHaveBeenCalled();expect(queue.records[0].error).toBe('upload session changed');queue.close();
 });
});

describe('upload source final integrity',()=>{
 it('refuses Complete when the source has changed after PUT',async()=>{
  let failed!:()=>void;const failure=new Promise<void>(r=>{failed=r});
  const complete=vi.fn(async()=>{});
  const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:9,parts_total:1}),sign:async()=>[{part_no:1,url:'fixture:part'}],put:async()=> 'etag',complete,abort:async()=>{}};
  const queue=new UploadQueue(api,{load:async()=>[],save:async rows=>{if(rows.some(r=>r.state==='error'))failed()}});
  const record=await queue.enqueue({...source(),verify:async()=> 'b'.repeat(64)},{tenant:'t',projectId:'p'});
  await failure;
  expect(record.error).toBe('UPLOAD_FILE_MISMATCH');expect(complete).not.toHaveBeenCalled();queue.close();
 });
});

describe('multipart checkpoint admission',()=>{
 const record=():UploadRecord=>({localId:'checkpoint',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),uploadId:'u',partBytes:4,partsTotal:3,parts:[{part_no:1,etag:'one'}],state:'paused'});
 it.each([
  {parts:[{part_no:1,etag:'a'},{part_no:1,etag:'b'}]},
  {parts:[{part_no:0,etag:'a'}]},
  {parts:[{part_no:4,etag:'a'}]},
  {parts:[{part_no:1.5,etag:'a'}]},
  {parts:[{part_no:1,etag:''}]},
  {parts:[{part_no:1,etag:'bad\r\netag'}]},
  {partBytes:0}, {partsTotal:2}, {uploadId:undefined},
 ])('rejects corrupt persisted layout/parts before replacing records: %j',async patch=>{
  const save=vi.fn(async()=>{}),queue=new UploadQueue({} as MultipartAPI,{load:async()=>[{...record(),...patch}],save});
  await expect(queue.restore()).rejects.toThrow('UPLOAD_CHECKPOINT_INVALID');
  expect(queue.records).toEqual([]);expect(save).not.toHaveBeenCalled();queue.close();
 });
 it('owns restored part objects and orders them',async()=>{
  const stored=record();stored.parts=[{part_no:3,etag:'three'},{part_no:1,etag:'one'}];
  const queue=new UploadQueue({} as MultipartAPI,{load:async()=>[stored],save:async()=>{}});
  await queue.restore();stored.parts[0].etag='mutated';
  expect(queue.records[0].parts).toEqual([{part_no:1,etag:'one'},{part_no:3,etag:'three'}]);queue.close();
 });
 it('rejects duplicate local identities atomically',async()=>{
  const queue=new UploadQueue({} as MultipartAPI,{load:async()=>[record(),record()],save:async()=>{}});
  await expect(queue.restore()).rejects.toThrow('UPLOAD_CHECKPOINT_INVALID');expect(queue.records).toEqual([]);queue.close();
 });
 it.each([NaN,1.5,Infinity])('rejects noninteger concurrency %s',value=>{
  const storage={load:async()=>[],save:async()=>{}};
  expect(()=>new UploadQueue({} as MultipartAPI,storage,value,2)).toThrow('unsafe upload concurrency');
  expect(()=>new UploadQueue({} as MultipartAPI,storage,2,value)).toThrow('unsafe upload concurrency');
 });
});

describe('upload queue concurrent lifecycle',()=>{
 it('serializes durable snapshots and skips pending writes after close',async()=>{
  let release!:()=>void,entered!:()=>void;
  const gate=new Promise<void>(r=>{release=r}),started=new Promise<void>(r=>{entered=r});
  const writes:UploadRecord[][]=[];
  const queue=new UploadQueue({} as MultipartAPI,{load:async()=>[],save:async rows=>{writes.push(structuredClone(rows));entered();await gate}});
  const first=queue.enqueue(source(),{tenant:'t',projectId:'p'});await started;
  const second=queue.enqueue(source(),{tenant:'t',projectId:'p'});
  await Promise.resolve();expect(writes).toHaveLength(1);
  queue.close();release();await Promise.all([first,second]);expect(writes).toHaveLength(1);
 });
 it('waits for a paused attempt to settle before restarting the same record',async()=>{
  let release!:()=>void,entered!:()=>void,finished!:()=>void;
  const gate=new Promise<void>(r=>{release=r}),started=new Promise<void>(r=>{entered=r}),done=new Promise<void>(r=>{finished=r});
  let calls=0,active=0,peak=0;
  const begin=vi.fn(async()=>{calls++;active++;peak=Math.max(peak,active);if(calls===1){entered();await gate}active--;return{id:'u',part_bytes:9,parts_total:1}});
  const api:MultipartAPI={begin,sign:async()=>[{part_no:1,url:'fixture:part'}],put:async()=> 'etag',complete:async()=>{},abort:async()=>{}};
  const queue=new UploadQueue(api,{load:async()=>[],save:async rows=>{if(rows.some(r=>r.state==='complete'))finished()}});
  const record=await queue.enqueue(source(),{tenant:'t',projectId:'p'});await started;
  await expect(queue.resume(record.localId,source())).rejects.toThrow('UPLOAD_BUSY');
  await queue.pause(record.localId);await queue.resume(record.localId,source());
  expect(begin).toHaveBeenCalledTimes(1);release();await done;
  expect(begin).toHaveBeenCalledTimes(2);expect(peak).toBe(1);queue.close();
 });
});

describe('upload abort lifecycle',()=>{
 const checkpoint=():UploadRecord=>({localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),uploadId:'u',partBytes:9,partsTotal:1,parts:[],state:'paused'});
 it('fences a stale status response when abort wins',async()=>{
  let entered!:()=>void,release!:()=>void;
  const started=new Promise<void>(r=>{entered=r}),gate=new Promise<void>(r=>{release=r});
  let statusCalls=0;const status=vi.fn(async()=>{statusCalls++;if(statusCalls===1){entered();await gate}return{id:'u',part_bytes:9,parts_total:1,state:statusCalls===1?'verified' as const:'uploading' as const}});
  const api={status,abort:async()=>{}} as unknown as MultipartAPI;
  const queue=new UploadQueue(api,{load:async()=>[checkpoint()],save:async()=>{}});await queue.restore();
  const refreshing=queue.refresh('l');const rejected=expect(refreshing).rejects.toThrow();await started;
  await queue.abort('l');release();await rejected;
  expect(queue.records[0].state).toBe('aborted');await queue.refresh('l');expect(status).toHaveBeenCalledTimes(2);queue.close();
 });
 it('cancels Abort transport and fences its late response after close',async()=>{
  let entered!:()=>void,release!:()=>void,signal:AbortSignal|undefined;
  const started=new Promise<void>(r=>{entered=r}),gate=new Promise<void>(r=>{release=r});
  const queue=new UploadQueue({abort:async(_id:string,s:AbortSignal)=>{signal=s;entered();await gate}} as MultipartAPI,{load:async()=>[checkpoint()],save:async()=>{}});
  await queue.restore();const aborting=queue.abort('l');const rejected=expect(aborting).rejects.toThrow('UPLOAD_QUEUE_CLOSED');await started;
  await expect(queue.resume('l',source())).rejects.toThrow('UPLOAD_BUSY');queue.close();expect(signal?.aborted).toBe(true);release();await rejected;expect(queue.records[0].state).toBe('paused');
 });
});

describe('multipart batch settlement',()=>{
 it('retains the running attempt until every sibling PUT settles after a failure',async()=>{
  let release!:()=>void,entered!:()=>void,failed!:()=>void;
  const gate=new Promise<void>(r=>{release=r}),started=new Promise<void>(r=>{entered=r}),failure=new Promise<void>(r=>{failed=r});
  const complete=vi.fn(async()=>{}),sign=vi.fn(async(_id:string,parts:number[])=>parts.map((part_no:number)=>({part_no,url:String(part_no)})));
  const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:4,parts_total:3}),sign,put:async url=>{
   if(url==='1')throw new Error('part failed');entered();await gate;return 'etag';
  },complete,abort:async()=>{}};
  const queue=new UploadQueue(api,{load:async()=>[],save:async rows=>{if(rows.some(r=>r.state==='error'))failed()}});
  const record=await queue.enqueue(source(),{tenant:'t',projectId:'p'});await started;
  for(let i=0;i<10;i++)await Promise.resolve();
  expect(record.state).toBe('uploading');await expect(queue.resume(record.localId,source())).rejects.toThrow('UPLOAD_BUSY');
  expect(sign).toHaveBeenCalledTimes(1);release();await failure;
  expect(record.error).toBe('part failed');expect(record.parts).toEqual([]);expect(complete).not.toHaveBeenCalled();queue.close();
 });
 it('drains a delayed sibling after pause before restarting',async()=>{
  let release!:()=>void,entered!:()=>void,finished!:()=>void;let delayed=true;
  const gate=new Promise<void>(r=>{release=r}),started=new Promise<void>(r=>{entered=r}),done=new Promise<void>(r=>{finished=r});
  const sign=vi.fn(async(_id:string,parts:number[])=>parts.map((part_no:number)=>({part_no,url:String(part_no)})));
  const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:4,parts_total:3}),sign,put:async url=>{
   if(delayed&&url==='2'){entered();await gate}return 'etag';
  },complete:async()=>{},abort:async()=>{}};
  const queue=new UploadQueue(api,{load:async()=>[],save:async rows=>{if(rows.some(r=>r.state==='complete'))finished()}});
  const record=await queue.enqueue(source(),{tenant:'t',projectId:'p'});await started;
  await queue.pause(record.localId);await queue.resume(record.localId,source());expect(sign).toHaveBeenCalledTimes(1);
  delayed=false;release();await done;expect(record.state).toBe('complete');queue.close();
 });
});

describe('Abort recovers unknown Begin',()=>{
 const pendingRecord=():UploadRecord=>({localId:'original-key',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),parts:[],state:'paused'});
 it('replays the durable key and saves recovered identity before Abort without reading parts',async()=>{
  let stored:UploadRecord[]=[];
  const begin=vi.fn(async(r:UploadRecord)=>{expect(r.localId).toBe('original-key');return{id:'recovered',part_bytes:9,parts_total:1}});
  const abort=vi.fn(async(id:string)=>{expect(id).toBe('recovered');expect(stored[0].uploadId).toBe(id)});
  const sign=vi.fn(),put=vi.fn(),complete=vi.fn();
  const queue=new UploadQueue({begin,abort,sign,put,complete},{load:async()=>[pendingRecord()],save:async rows=>{stored=structuredClone(rows)}});
  await queue.restore();await queue.abort('original-key');expect(queue.records[0].state).toBe('aborted');expect(begin).toHaveBeenCalledTimes(1);expect(abort).toHaveBeenCalledTimes(1);expect(sign).not.toHaveBeenCalled();expect(put).not.toHaveBeenCalled();expect(complete).not.toHaveBeenCalled();queue.close();
 });
 it('preserves the original key on recovery failure and retries it',async()=>{
  const begin=vi.fn().mockRejectedValueOnce(new Error('offline')).mockResolvedValue({id:'recovered',part_bytes:9,parts_total:1});const abort=vi.fn(async()=>{});
  const queue=new UploadQueue({begin,abort} as unknown as MultipartAPI,{load:async()=>[pendingRecord()],save:async()=>{}});
  await queue.restore();await expect(queue.abort('original-key')).rejects.toThrow('offline');expect(queue.records[0].state).toBe('error');expect(abort).not.toHaveBeenCalled();await queue.abort('original-key');expect(begin.mock.calls.map(call=>call[0].localId)).toEqual(['original-key','original-key']);expect(queue.records[0].state).toBe('aborted');queue.close();
 });
 it('refuses malformed recovered layout without adopting its identity or calling Abort',async()=>{
  const abort=vi.fn(async()=>{});const queue=new UploadQueue({begin:async()=>({id:'recovered',part_bytes:0,parts_total:1}),abort} as unknown as MultipartAPI,{load:async()=>[pendingRecord()],save:async()=>{}});
  await queue.restore();await expect(queue.abort('original-key')).rejects.toThrow('UPLOAD_CHECKPOINT_INVALID');expect(queue.records[0].uploadId).toBeUndefined();expect(abort).not.toHaveBeenCalled();queue.close();
 });
});

it('reconciles a lost Complete from persisted error without a local source',async()=>{
 const record:UploadRecord={localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),uploadId:'u',partBytes:9,partsTotal:1,parts:[{part_no:1,etag:'etag'}],state:'error',error:'network error'};
 const complete=vi.fn(),sign=vi.fn();const queue=new UploadQueue({status:async()=>({id:'u',part_bytes:9,parts_total:1,state:'verified'}),complete,sign} as unknown as MultipartAPI,{load:async()=>[record],save:async()=>{}});
 await queue.restore();await queue.refresh('l');expect(queue.records[0].state).toBe('complete');expect(queue.records[0].error).toBeUndefined();expect(complete).not.toHaveBeenCalled();expect(sign).not.toHaveBeenCalled();queue.close();
});

it.each(['UPLOAD_EXPIRED','UPLOAD_REJECTED'])('requires a new upload key for %s',async error=>{
 const record:UploadRecord={localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),uploadId:'u',partBytes:9,partsTotal:1,parts:[],state:'error',error};
 const queue=new UploadQueue({} as MultipartAPI,{load:async()=>[record],save:async()=>{}});await queue.restore();await expect(queue.resume('l',source())).rejects.toThrow('UPLOAD_RESTART_REQUIRED');expect(queue.records[0].error).toBe(error);queue.close();
});
it('reconciles verifying instead of sending an invalid server Abort',async()=>{
 const record:UploadRecord={localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),uploadId:'u',partBytes:9,partsTotal:1,parts:[],state:'error',error:'network error'};
 const abort=vi.fn();const queue=new UploadQueue({status:async()=>({id:'u',part_bytes:9,parts_total:1,state:'verifying'}),abort} as unknown as MultipartAPI,{load:async()=>[record],save:async()=>{}});await queue.restore();await queue.abort('l');expect(abort).not.toHaveBeenCalled();expect(queue.records[0].state).toBe('completing');queue.close();
});

describe('persisted upload metadata',()=>{
 it.each([{name:null},{name:'../frame.jpg'},{name:'x'.repeat(256)},{size:0},{size:NaN},{lastModified:-1},{lastModified:0.5},{sha256:'a'.repeat(63)},{sha256:'A'.repeat(64)},{state:'verified'},{error:{}},{tenant:''},{state:'complete',uploadId:undefined}])('rejects corrupt metadata atomically: %j',async patch=>{
  const record={localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),parts:[],state:'paused',...patch} as unknown as UploadRecord;
  const save=vi.fn(async()=>{}),queue=new UploadQueue({} as MultipartAPI,{load:async()=>[record],save});await expect(queue.restore()).rejects.toThrow('UPLOAD_CHECKPOINT_INVALID');expect(queue.records).toEqual([]);expect(save).not.toHaveBeenCalled();queue.close();
 });
});

describe('exclusive upload restore',()=>{
 it('refuses concurrent restore and mutations while loading',async()=>{
  let release!:(rows:UploadRecord[])=>void;const load=vi.fn(()=>new Promise<UploadRecord[]>(r=>{release=r}));
  const queue=new UploadQueue({} as MultipartAPI,{load,save:async()=>{}});const restoring=queue.restore();
  await expect(queue.restore()).rejects.toThrow('UPLOAD_RESTORE_IN_PROGRESS');
  await expect(queue.enqueue(source(),{tenant:'t',projectId:'p'})).rejects.toThrow('UPLOAD_RESTORE_IN_PROGRESS');
  await expect(queue.refresh('l')).rejects.toThrow('UPLOAD_RESTORE_IN_PROGRESS');
  expect(load).toHaveBeenCalledTimes(1);release([]);await restoring;expect(queue.records).toEqual([]);queue.close();
 });
 it('releases restore lock after a load failure for explicit retry',async()=>{
  const load=vi.fn().mockRejectedValueOnce(new Error('storage')).mockResolvedValueOnce([]);
  const queue=new UploadQueue({} as MultipartAPI,{load,save:async()=>{}});await expect(queue.restore()).rejects.toThrow('storage');await expect(queue.restore()).resolves.toEqual([]);queue.close();
 });
 it('does not replace an already installed queue on repeated restore',async()=>{
  const row:UploadRecord={localId:'l',tenant:'t',projectId:'p',name:'x.jpg',size:9,lastModified:7,sha256:'a'.repeat(64),parts:[],state:'paused'};
  const load=vi.fn(async()=>[row]);const queue=new UploadQueue({} as MultipartAPI,{load,save:async()=>{}});await queue.restore();await expect(queue.restore()).rejects.toThrow('UPLOAD_BUSY');expect(load).toHaveBeenCalledTimes(1);expect(queue.records[0].localId).toBe('l');queue.close();
 });
});


describe('admitted upload source ownership',()=>{
 it.each(['name','size','sha256','read','verify'])('refuses caller mutation of %s while Begin waits',async field=>{
  let entered!:()=>void,release!:()=>void;const started=new Promise<void>(r=>{entered=r}),gate=new Promise<void>(r=>{release=r});
  const api:MultipartAPI={begin:async()=>{entered();await gate;return{id:'u',part_bytes:9,parts_total:1}},sign:async()=>[{part_no:1,url:'fixture:part'}],put:vi.fn(async()=>'etag'),complete:vi.fn(async()=>{}),abort:async()=>{}};
  const file={...source(),verify:async()=>'a'.repeat(64)};const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});
  await queue.enqueue(file,{tenant:'t',projectId:'p'});await started;
  if(field==='name')file.name='changed.jpg';else if(field==='size')file.size=8;else if(field==='sha256')file.sha256='b'.repeat(64);else if(field==='read')file.read=async()=>new Uint8Array(9);else file.verify=async()=>'b'.repeat(64);
  release();for(let i=0;i<60&&queue.records[0].state!=='error';i++)await Promise.resolve();
  expect(queue.records[0].error).toBe('UPLOAD_FILE_MISMATCH');expect(api.put).not.toHaveBeenCalled();expect(api.complete).not.toHaveBeenCalled();queue.close();
 });
});


it('isolates Begin adapter writes from durable identity, policy and checkpoint',async()=>{
 let captured:UploadRecord|undefined;
 const api:MultipartAPI={begin:async record=>{captured=record;record.name='foreign.nef';record.sha256='b'.repeat(64);record.parts.push({part_no:1,etag:'foreign'});if(record.rawDevelopment)record.rawDevelopment.exposure_ev=4;throw new Error('offline')},sign:vi.fn(),put:vi.fn(),complete:vi.fn(),abort:vi.fn()};
 const policy={schema_version:1 as const,demosaic:'ahd' as const,wb_source:'camera' as const,exposure_ev:1,highlight_policy:'blend' as const,decoder_version:'rawpy-0.27.1/libraw-0.21.4'};
 const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});
 const record=await queue.enqueue({...source(),name:'frame.nef'},{tenant:'t',projectId:'p',rawDevelopment:policy});
 for(let i=0;i<40&&record.state!=='error';i++)await Promise.resolve();
 expect(captured).not.toBe(record);expect(record.name).toBe('frame.nef');expect(record.sha256).toBe('a'.repeat(64));expect(record.parts).toEqual([]);expect(record.rawDevelopment?.exposure_ev).toBe(1);expect(record.error).toBe('offline');queue.close();
});


it('does not let signing adapter redefine requested part membership',async()=>{
 const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:9,parts_total:1}),sign:async(_id,parts)=>{parts[0]=2;return[{part_no:2,url:'foreign:part'}]},put:vi.fn(),complete:vi.fn(),abort:async()=>{}};
 const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});await queue.enqueue(source(),{tenant:'t',projectId:'p'});
 for(let i=0;i<40&&queue.records[0].state!=='error';i++)await Promise.resolve();
 expect(queue.records[0].error).toBe('invalid signed part batch');expect(api.put).not.toHaveBeenCalled();expect(api.complete).not.toHaveBeenCalled();queue.close();
});
it('owns signed part number and URL across an awaited source read',async()=>{
 let entered!:()=>void,release!:()=>void;const started=new Promise<void>(r=>{entered=r}),gate=new Promise<void>(r=>{release=r});
 const link={part_no:1,url:'fixture:original'};
 const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:9,parts_total:1}),sign:async()=>[link],put:vi.fn(async()=>'etag'),complete:vi.fn(async()=>{}),abort:async()=>{}};
 const file={...source(),read:async()=>{entered();await gate;return new Uint8Array(9)}};
 const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});await queue.enqueue(file,{tenant:'t',projectId:'p'});await started;
 link.part_no=2;link.url='fixture:substituted';release();
 for(let i=0;i<60&&queue.records[0].state!=='complete';i++)await Promise.resolve();
 expect(queue.records[0].state).toBe('complete');expect(api.put).toHaveBeenCalledWith('fixture:original',expect.any(Uint8Array),expect.any(AbortSignal));expect(queue.records[0].parts).toEqual([{part_no:1,etag:'etag'}]);queue.close();
});


it.each([null,new ArrayBuffer(9),{byteLength:9},new Uint16Array(9)])('refuses a non-byte source result before PUT: %j',async result=>{
 const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:9,parts_total:1}),sign:async()=>[{part_no:1,url:'fixture:part'}],put:vi.fn(),complete:vi.fn(),abort:async()=>{}};
 const file={...source(),read:async()=>result as unknown as Uint8Array};const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});await queue.enqueue(file,{tenant:'t',projectId:'p'});
 for(let i=0;i<50&&queue.records[0].state!=='error';i++)await Promise.resolve();
 expect(queue.records[0].error).toBe('part size mismatch');expect(api.put).not.toHaveBeenCalled();expect(api.complete).not.toHaveBeenCalled();queue.close();
});
it('isolates in-flight PUT bytes from a source-owned reusable buffer',async()=>{
 const backing=new Uint8Array([9,1,2,3,9]);const bytes=backing.subarray(1,4);
 let entered!:()=>void,release!:()=>void;const started=new Promise<void>(r=>{entered=r}),gate=new Promise<void>(r=>{release=r});let sent:Uint8Array|undefined;
 const api:MultipartAPI={begin:async()=>({id:'u',part_bytes:3,parts_total:1}),sign:async()=>[{part_no:1,url:'fixture:part'}],put:async(_url,value)=>{sent=value;entered();await gate;expect([...value]).toEqual([1,2,3]);return'etag'},complete:vi.fn(async()=>{}),abort:async()=>{}};
 const file={...source(),size:3,read:async()=>bytes};const queue=new UploadQueue(api,{load:async()=>[],save:async()=>{}});await queue.enqueue(file,{tenant:'t',projectId:'p'});await started;
 expect(sent).not.toBe(bytes);expect(sent?.byteLength).toBe(3);backing.fill(7);release();
 for(let i=0;i<60&&queue.records[0].state!=='complete';i++)await Promise.resolve();expect(queue.records[0].state).toBe('complete');queue.close();
});

describe('explicit missing ICC policy',()=>{
 it.each(['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1'] as const)('persists and restores %s before Begin',async assumeProfile=>{
  let stored:UploadRecord[]=[];let begun:UploadRecord|undefined;
  const persistence:UploadPersistence={load:async()=>stored,save:async rows=>{stored=structuredClone(rows)}};
  const api={begin:async(row:UploadRecord)=>{begun=structuredClone(row);throw new Error('offline')}} as unknown as MultipartAPI;
  const q=new UploadQueue(api,persistence);const scope={tenant:'t',projectId:'p',assumeProfile};await q.enqueue(source(),scope);scope.assumeProfile='srgb_v1';
  for(let i=0;i<20&&!begun;i++)await Promise.resolve();expect(begun?.assumeProfile).toBe(assumeProfile);
  q.close();const restored=new UploadQueue(api,persistence);await restored.restore();expect(restored.records[0].assumeProfile).toBe(assumeProfile);restored.close();
 });
 it.each([null,'sRGB','unknown',{},5])('refuses malformed persisted profile %s',async assumeProfile=>{
  const row={localId:'l',tenant:'t',projectId:'p',...source(),parts:[],state:'paused',assumeProfile} as unknown as UploadRecord;
  const save=vi.fn();const q=new UploadQueue({} as MultipartAPI,{load:async()=>[row],save});await expect(q.restore()).rejects.toThrow('UPLOAD_PROFILE_INVALID');expect(q.records).toEqual([]);expect(save).not.toHaveBeenCalled();
 });
});
