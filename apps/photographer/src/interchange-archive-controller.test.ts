import {expect,it,vi} from 'vitest';
import {runArchiveUpload,checkArchiveAttempt,abortArchiveUpload,resetAbortedArchiveUpload,openReadyArchiveUpload} from './interchange-archive-controller';
import {InterchangeArchiveAttemptStore} from './interchange-archive-attempt-store';
import {archiveRequestSHA,archiveUploadID,archiveArtifactID,type ArchiveUploadReceipt} from './interchange-archive-upload';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const lock=async<T>(_key:string,fn:()=>Promise<T>)=>fn();
it('checks an unreceipted saved attempt without calling Begin or storage PUT',async()=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)},project=id(2),tenant=id(1),user=id(9);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://attempt-read.test',user,tenant,project,request,lock);
 await journal.write({version:1,key:'saved-key',body:request,checkpoint:null});
 const status={upload_id:archiveUploadID(tenant,user,'saved-key'),state:'reserved',request_sha256:archiveRequestSHA(project,request),expires_at:new Date(Date.now()+3600000).toISOString(),ready:false,begin_recovery:'reconciliation_required' as const};
 const read=vi.fn(async(actor:string,pid:string,body:typeof request,key:string)=>{expect([actor,pid,key]).toEqual([user,project,'saved-key']);expect(body).toEqual(request);return status;});
 const result=await checkArchiveAttempt({api:{interchangeArchiveAttemptStatus:read},journal,user,tenant,project,request,signal:new AbortController().signal,lock});
 expect(result).toEqual(status);expect(read).toHaveBeenCalledOnce();expect((await journal.read())?.checkpoint).toBeNull();
 await expect(checkArchiveAttempt({api:{interchangeArchiveAttemptStatus:async()=>({...status,upload_id:id(77)})},journal,user,tenant,project,request,signal:new AbortController().signal,lock})).rejects.toThrow('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
 await expect(checkArchiveAttempt({api:{interchangeArchiveAttemptStatus:async()=>({...status,begin_recovery:'unknown' as never})},journal,user,tenant,project,request,signal:new AbortController().signal,lock})).rejects.toThrow('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
});
it('persists intent before Begin and resumes queued without repeated PUT or Complete',async()=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)},project=id(2),tenant=id(1);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://archive.test',id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 const api={beginInterchangeArchive:vi.fn(async()=>{expect((await journal.read())?.key).toBe('original');return receipt;}),interchangeArchiveParts:vi.fn(async()=>[{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}]),completeInterchangeArchive:vi.fn(async()=>({upload_id:id(3),accepted:true})),interchangeArchiveStatus:vi.fn(async()=>({upload_id:id(3),state:'completing',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}))};
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:async()=>new Uint8Array(22)},put=vi.fn(async()=>{expect((await journal.read())?.checkpoint?.receipt.upload_id).toBe(id(3));return 'etag';}),newKey=vi.fn(()=>'original');
 const options={api,journal,tenant,project,request,source,signal:new AbortController().signal,put,newKey,lock};
 await runArchiveUpload(options);await runArchiveUpload(options);
 expect(api.beginInterchangeArchive).toHaveBeenCalledOnce();expect(put).toHaveBeenCalledOnce();expect(api.completeInterchangeArchive).toHaveBeenCalledOnce();expect(newKey).toHaveBeenCalledOnce();expect((await journal.read())?.checkpoint?.stage).toBe('queued');
});

it('persists abort intent before lost response and retries the same upload',async()=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'cancel.zip',bytes:22,sha256:'b'.repeat(64)},project=id(5),tenant=id(4);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://cancel.test',id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(6),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'original',body:request,checkpoint:{receipt,parts:[],stage:'uploading'}});
 const abort=vi.fn(async()=>{expect((await journal.read())?.checkpoint?.stage).toBe('aborting');throw new Error('lost abort reply');});
 const status=vi.fn(async()=>({upload_id:id(6),state:'aborting',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false,error_code:'UPLOAD_ABORTED'}));
 const options={api:{abortInterchangeArchive:abort,interchangeArchiveStatus:status},journal,tenant,project,request,signal:new AbortController().signal,lock};
 await expect(abortArchiveUpload(options)).rejects.toThrow('lost abort reply');expect((await journal.read())?.checkpoint?.stage).toBe('aborting');expect(status).not.toHaveBeenCalled();
 const retry=vi.fn(async()=>({upload_id:id(6),accepted:true,cleanup_pending:true}));
 const out=await abortArchiveUpload({...options,api:{abortInterchangeArchive:retry,interchangeArchiveStatus:status}});
 expect(out.state).toBe('aborting');expect(retry.mock.calls[0]).toBeDefined();expect((await journal.read())?.key).toBe('original');
});
it('does not send Abort without a durable Begin acknowledgement',async()=>{
 const storage={getItem:async()=>null,setItem:async()=>{},removeItem:async()=>{}},request={filename:'empty.zip',bytes:22,sha256:'c'.repeat(64)};
 const journal=new InterchangeArchiveAttemptStore(storage,'https://no-begin.test',id(9),id(1),id(2),request,lock),abort=vi.fn(),status=vi.fn();
 await expect(abortArchiveUpload({api:{abortInterchangeArchive:abort,interchangeArchiveStatus:status},journal,tenant:id(1),project:id(2),request,signal:new AbortController().signal,lock})).rejects.toThrow('INTERCHANGE_ARCHIVE_ABORT_REQUIRES_BEGIN');expect(abort).not.toHaveBeenCalled();
});

it.each(['aborted','aborting','verified','foreign'])('resets only freshly confirmed aborted attempt: %s',async mode=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'reset.zip',bytes:22,sha256:'d'.repeat(64)},project=id(2),tenant=id(1);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://reset.test/'+mode,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'original',body:request,checkpoint:{receipt,parts:[],stage:'aborting'}});
 const status=vi.fn(async()=>({upload_id:mode==='foreign'?id(4):id(3),state:mode==='foreign'?'aborted':mode,request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}));
 const action=resetAbortedArchiveUpload({api:{interchangeArchiveStatus:status},journal,tenant,project,request,signal:new AbortController().signal,lock});
 if(mode==='aborted'){await action;expect(await journal.read()).toBeNull();}else{await expect(action).rejects.toThrow();expect((await journal.read())?.key).toBe('original');}
 expect(status).toHaveBeenCalledOnce();
});

it.each(['aborting','aborted','completing','verified','failed','expired','uploading'])('freshly checks saved uploading authority before resume: %s',async mode=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'resume.zip',bytes:22,sha256:'e'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://resume.test/'+mode,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'saved',body:request,checkpoint:{receipt,parts:[],stage:'uploading'}});
 const events:string[]=[],source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>new Uint8Array(22))};
 const api={beginInterchangeArchive:vi.fn(async()=>receipt),interchangeArchiveStatus:vi.fn(async()=>{events.push('status');return {upload_id:id(3),state:mode==='expired'?'uploading':mode,request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+(mode==='expired'?-1000:3600000)).toISOString(),ready:false};}),interchangeArchiveParts:vi.fn(async()=>{events.push('parts');return [{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}];}),completeInterchangeArchive:vi.fn(async()=>({upload_id:id(3),accepted:true}))};
 const put=vi.fn(async()=> 'etag'),newKey=vi.fn(()=> 'new');
 await runArchiveUpload({api,journal,tenant,project,request,source,signal:new AbortController().signal,put,newKey,lock});
 expect(api.beginInterchangeArchive).not.toHaveBeenCalled();expect(newKey).not.toHaveBeenCalled();expect(events[0]).toBe('status');
 if(mode==='uploading'){expect(put).toHaveBeenCalledOnce();expect(events[1]).toBe('parts');}
 else{expect(put).not.toHaveBeenCalled();expect(source.read).not.toHaveBeenCalled();expect(api.interchangeArchiveParts).not.toHaveBeenCalled();expect(api.completeInterchangeArchive).not.toHaveBeenCalled();expect((await journal.read())?.checkpoint?.stage).toBe('uploading');}
});

it.each([true,false])('recovers a lost Complete reply without reuploading durable parts; server accepted=%s',async accepted=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'lost-complete.zip',bytes:22,sha256:'f'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://lost-complete.test/'+accepted,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 let state='uploading';
 const api={
  beginInterchangeArchive:vi.fn(async()=>receipt),
  interchangeArchiveParts:vi.fn(async()=>[{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}]),
  completeInterchangeArchive:vi.fn(async()=>{state='completing';return {upload_id:id(3),accepted:true};}),
  interchangeArchiveStatus:vi.fn(async()=>({upload_id:id(3),state,request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false})),
 };
 api.completeInterchangeArchive.mockImplementationOnce(async()=>{if(accepted)state='completing';throw new Error('Complete response lost');});
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>new Uint8Array(22))},put=vi.fn(async()=> 'durable-etag'),newKey=vi.fn(()=> 'stable-complete-key');
 const options={api,journal,tenant,project,request,source,signal:new AbortController().signal,put,newKey,lock};
 await expect(runArchiveUpload(options)).rejects.toThrow('Complete response lost');
 const saved=await journal.read();expect(saved?.checkpoint?.parts).toEqual([{part_no:1,etag:'durable-etag'}]);expect(saved?.checkpoint?.stage).toBe('uploading');
 const result=await runArchiveUpload(options);expect(result.state).toBe('completing');expect(result.ready).toBe(false);
 expect(api.beginInterchangeArchive).toHaveBeenCalledOnce();expect(api.interchangeArchiveParts).toHaveBeenCalledOnce();expect(source.read).toHaveBeenCalledOnce();expect(put).toHaveBeenCalledOnce();expect(newKey).toHaveBeenCalledOnce();
 expect(api.completeInterchangeArchive).toHaveBeenCalledTimes(accepted?1:2);
 expect((await journal.read())?.key).toBe('stable-complete-key');
 expect((await journal.read())?.checkpoint?.stage).toBe(accepted?'uploading':'queued');
});

it.each(['lost-response','receipt-storage','removed-intent'])('keeps Begin retry bound to durable intent: %s',async mode=>{
 const values=new Map<string,string>();let rejectReceipt=mode==='receipt-storage';
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{if(rejectReceipt&&k.includes('attempt')&&JSON.parse(v).checkpoint){rejectReceipt=false;throw new Error('receipt storage failed');}values.set(k,v);},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'begin-retry.zip',bytes:22,sha256:'a'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://begin-retry.test/'+mode,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 const api={beginInterchangeArchive:vi.fn(async(_project:string,_request:typeof request,_key:string,_signal:AbortSignal)=>receipt),interchangeArchiveParts:vi.fn(async()=>[{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}]),completeInterchangeArchive:vi.fn(async()=>({upload_id:id(3),accepted:true})),interchangeArchiveStatus:vi.fn(async()=>({upload_id:id(3),state:'completing',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}))};
 if(mode==='lost-response')api.beginInterchangeArchive.mockRejectedValueOnce(new Error('Begin response lost'));
 if(mode==='removed-intent')api.beginInterchangeArchive.mockImplementationOnce(async()=>{values.delete(journal.key);return receipt;});
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>new Uint8Array(22))},put=vi.fn(async()=> 'etag'),newKey=vi.fn(()=> 'original-begin-key');
 const options={api,journal,tenant,project,request,source,signal:new AbortController().signal,put,newKey,lock};
 await expect(runArchiveUpload(options)).rejects.toThrow(mode==='removed-intent'?'INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT':mode==='receipt-storage'?'receipt storage failed':'Begin response lost');
 expect(put).not.toHaveBeenCalled();expect(api.interchangeArchiveParts).not.toHaveBeenCalled();expect(api.completeInterchangeArchive).not.toHaveBeenCalled();
 if(mode==='removed-intent'){expect(await journal.read()).toBeNull();return;}
 expect((await journal.read())?.key).toBe('original-begin-key');expect((await journal.read())?.checkpoint).toBeNull();
 await runArchiveUpload(options);expect(newKey).toHaveBeenCalledOnce();expect(api.beginInterchangeArchive).toHaveBeenCalledTimes(2);
 for(const call of api.beginInterchangeArchive.mock.calls)expect(call[2]).toBe('original-begin-key');
 expect(put).toHaveBeenCalledOnce();expect((await journal.read())?.checkpoint?.stage).toBe('queued');
});

it.each(['etag','receipt-state','queued'])('rejects durable attempt mutation during part authorization: %s',async mode=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'fence.zip',bytes:22,sha256:'b'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://full-fence.test/'+mode,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 const api={beginInterchangeArchive:vi.fn(async()=>receipt),interchangeArchiveParts:vi.fn(async()=>{
  const record=JSON.parse(values.get(journal.key)!);
  if(mode==='etag')record.checkpoint.parts=[{part_no:1,etag:'foreign'}];
  if(mode==='receipt-state')record.checkpoint.receipt.state='completing';
  if(mode==='queued'){record.checkpoint.parts=[{part_no:1,etag:'foreign'}];record.checkpoint.stage='queued';}
  values.set(journal.key,JSON.stringify(record));
  return [{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}];
 }),completeInterchangeArchive:vi.fn(async()=>({upload_id:id(3),accepted:true})),interchangeArchiveStatus:vi.fn(async()=>({upload_id:id(3),state:'completing',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}))};
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>new Uint8Array(22))},put=vi.fn(async()=> 'etag');
 await expect(runArchiveUpload({api,journal,tenant,project,request,source,signal:new AbortController().signal,put,newKey:()=> 'key',lock})).rejects.toThrow('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
 expect(put).not.toHaveBeenCalled();expect(source.read).not.toHaveBeenCalled();expect(api.completeInterchangeArchive).not.toHaveBeenCalled();
});

it.each(['abort-request','abort-status','reset-status'])('refuses changed checkpoint during cancellation lifecycle: %s',async phase=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'abort-fence.zip',bytes:22,sha256:'c'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://abort-fence.test/'+phase,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'stable',body:request,checkpoint:{receipt,parts:[],stage:'aborting'}});
 const mutate=()=>{const record=JSON.parse(values.get(journal.key)!);record.checkpoint.parts=[{part_no:1,etag:'foreign'}];values.set(journal.key,JSON.stringify(record));};
 const abort=vi.fn(async()=>{if(phase==='abort-request')mutate();return {upload_id:id(3),accepted:true,cleanup_pending:true};});
 const status=vi.fn(async()=>{if(phase!=='abort-request')mutate();return {upload_id:id(3),state:'aborted',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false,error_code:'UPLOAD_ABORTED'};});
 const options={api:{abortInterchangeArchive:abort,interchangeArchiveStatus:status},journal,tenant,project,request,signal:new AbortController().signal,lock};
 await expect(phase==='reset-status'?resetAbortedArchiveUpload(options):abortArchiveUpload(options)).rejects.toThrow('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
 expect((await journal.read())?.checkpoint?.parts).toEqual([{part_no:1,etag:'foreign'}]);expect((await journal.read())?.key).toBe('stable');
 if(phase==='abort-request')expect(status).not.toHaveBeenCalled();if(phase==='reset-status')expect(abort).not.toHaveBeenCalled();
});

it.each(['removed','rekeyed'])('refuses Begin after persisted intent changes before network: %s',async mode=>{
 const values=new Map<string,string>();let change=true;
 const storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{
  values.set(k,v);
  if(change&&k.includes('attempt')&&JSON.parse(v).checkpoint===null){change=false;if(mode==='removed')values.delete(k);else values.set(k,JSON.stringify({...JSON.parse(v),key:'foreign-key'}));}
 },removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'pre-begin.zip',bytes:22,sha256:'d'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://pre-begin.test/'+mode,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 const api={beginInterchangeArchive:vi.fn(async()=>receipt),interchangeArchiveParts:vi.fn(async()=>[]),completeInterchangeArchive:vi.fn(async()=>({upload_id:id(3),accepted:true})),interchangeArchiveStatus:vi.fn(async()=>({upload_id:id(3),state:'completing',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}))};
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>new Uint8Array(22))};
 await expect(runArchiveUpload({api,journal,tenant,project,request,source,signal:new AbortController().signal,newKey:()=> 'original-key',lock})).rejects.toThrow('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');
 expect(api.beginInterchangeArchive).not.toHaveBeenCalled();expect(api.interchangeArchiveParts).not.toHaveBeenCalled();expect(source.read).not.toHaveBeenCalled();
});


it.each(['valid','cancelled','deleted','pending'])('ready archive handoff keeps execution lock and refuses %s race',async mode=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'ready.zip',bytes:22,sha256:'a'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,`https://handoff-${mode}.test`,id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'ready-attempt',body:request,checkpoint:{receipt,parts:[{part_no:1,etag:'etag'}],stage:'queued'}});
 let held=false;
 const executionLock=async<T>(key:string,action:()=>Promise<T>)=>{expect(key).toBe('archive-execution:'+journal.key);held=true;try{return await action();}finally{held=false;}};
 const controller=new AbortController(),onReady=vi.fn(()=>{expect(held).toBe(true);});
 const onStatus=vi.fn((status:{ready:boolean;artifact?:{artifact_id:string;sha256:string}})=>{expect(held).toBe(true);if(status.artifact)status.artifact.sha256='f'.repeat(64);});
 const status=vi.fn(async()=>{
  expect(held).toBe(true);
  if(mode==='cancelled')controller.abort();
  if(mode==='deleted')values.delete(journal.key);
  return {upload_id:id(3),state:mode==='pending'?'completing':'verified',request_sha256:receipt.request_sha256,expires_at:'2030-01-01T00:00:00Z',ready:mode!=='pending',...(mode==='pending'?{}:{artifact:{artifact_id:archiveArtifactID(id(3)),sha256:request.sha256}})};
 });
 const result=openReadyArchiveUpload({api:{interchangeArchiveStatus:status},journal,tenant,project,request,signal:controller.signal,onStatus,onReady,lock:executionLock});
 if(mode==='valid'){await result;expect(onReady).toHaveBeenCalledWith({artifact_id:archiveArtifactID(id(3)),sha256:request.sha256});}else{await expect(result).rejects.toThrow();expect(onReady).not.toHaveBeenCalled();}
 expect(onStatus).toHaveBeenCalledTimes(mode==='valid'||mode==='pending'?1:0);
 expect(held).toBe(false);
});


it('serializes abort behind ready handoff and retains originally bound callbacks',async()=>{
 const values=new Map<string,string>(),storage={getItem:async(k:string)=>values.get(k)??null,setItem:async(k:string,v:string)=>{values.set(k,v)},removeItem:async(k:string)=>{values.delete(k)}};
 const request={filename:'serialized.zip',bytes:22,sha256:'a'.repeat(64)},tenant=id(1),project=id(2);
 const journal=new InterchangeArchiveAttemptStore(storage,'https://serialized-handoff.test',id(9),tenant,project,request,lock);
 const receipt:ArchiveUploadReceipt={upload_id:id(3),state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 await journal.write({version:1,key:'serialized',body:request,checkpoint:{receipt,parts:[{part_no:1,etag:'etag'}],stage:'queued'}});
 const events:string[]=[];
 let started!:()=>void,resolveStatus!:(value:unknown)=>void;
 const statusStarted=new Promise<void>(resolve=>{started=resolve;});
 const ready={upload_id:id(3),state:'verified',request_sha256:receipt.request_sha256,expires_at:'2030-01-01T00:00:00Z',ready:true,artifact:{artifact_id:archiveArtifactID(id(3)),sha256:request.sha256}};
 const injected=vi.fn();
 const options={api:{interchangeArchiveStatus:vi.fn(async()=>{events.push('read-ready');started();return await new Promise<unknown>(resolve=>{resolveStatus=resolve;});})},journal,tenant,project,request,signal:new AbortController().signal,onReady:vi.fn(()=>{events.push('handoff');}),lock};
 const handoff=openReadyArchiveUpload(options);await statusStarted;
 const original=options.onReady;options.onReady=injected;
 const abort=abortArchiveUpload({api:{abortInterchangeArchive:async()=>{events.push('abort');return {upload_id:id(3),accepted:true,cleanup_pending:true};},interchangeArchiveStatus:async()=>({...ready,state:'aborting',ready:false,artifact:undefined,error_code:'UPLOAD_ABORTED'})},journal,tenant,project,request,signal:new AbortController().signal,lock});
 expect(events).toEqual(['read-ready']);
 resolveStatus(ready);await handoff;await abort;
 expect(events).toEqual(['read-ready','handoff','abort']);
 expect(original).toHaveBeenCalledOnce();expect(injected).not.toHaveBeenCalled();
 expect((await journal.read())?.checkpoint?.stage).toBe('aborting');
});
