import {expect,it,vi} from 'vitest';
import {OwnerAPI} from './owner-api';
import {archiveArtifactID,archiveRequestSHA,readArchiveStatus} from './interchange-archive-upload';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request=()=>({filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)});
const receipt=()=>({upload_id:id(3),state:'uploading' as const,part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(id(2),request())});
for(const phase of ['begin','attempt','parts','complete','status','abort'] as const)it(`archive ${phase} rejects late and pre-aborted transport`,async()=>{
 let resolve!:(value:unknown)=>void;
 const transport=vi.fn(()=>new Promise<unknown>(done=>{resolve=done})),api=new OwnerAPI({request:transport} as never,id(1));
 const invoke=(signal:AbortSignal)=>phase==='begin'?api.beginInterchangeArchive(id(2),request(),'original-key',signal):phase==='attempt'?api.interchangeArchiveAttemptStatus(id(4),id(2),request(),'original-key',signal):phase==='parts'?api.interchangeArchiveParts(id(2),request(),receipt(),[1],signal):phase==='complete'?api.completeInterchangeArchive(id(2),request(),receipt(),[{part_no:1,etag:'part-etag'}],signal):phase==='status'?api.interchangeArchiveStatus(id(2),request(),receipt(),signal):api.abortInterchangeArchive(id(2),request(),receipt(),signal);
 const controller=new AbortController(),pending=invoke(controller.signal);
 controller.abort();resolve({unexpected:'late'});
 await expect(pending).rejects.toMatchObject({name:'AbortError'});
 await expect(invoke(controller.signal)).rejects.toMatchObject({name:'AbortError'});
 expect(transport).toHaveBeenCalledTimes(1);
});
it('exposes abort during an unresolved Begin generation without permitting upload actions',()=>{
 const pin=receipt(),status={upload_id:pin.upload_id,state:'aborting',request_sha256:pin.request_sha256,expires_at:'2026-10-17T10:00:00Z',ready:false,error_code:'UPLOAD_ABORTED',begin_recovery:'reconciliation_required'};
 expect(readArchiveStatus(status,pin,request()).begin_recovery).toBe('reconciliation_required');
 expect(()=>readArchiveStatus({...status,begin_recovery:'receipt_pending_bind'},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,state:'uploading'},pin,request())).toThrow();
});
it('binds ready status to the request captured before transport',async()=>{
 let resolve!:(value:unknown)=>void;
 const transport=vi.fn(()=>new Promise<unknown>(done=>{resolve=done}));
 const api=new OwnerAPI({request:transport} as never,id(1)),body=request(),pin=receipt();
 const pending=api.interchangeArchiveStatus(id(2),body,pin);
 body.sha256='b'.repeat(64);
 resolve({upload_id:pin.upload_id,state:'verified',request_sha256:pin.request_sha256,expires_at:'2026-10-17T10:00:00Z',ready:true,begin_recovery:'resolved',artifact:{artifact_id:archiveArtifactID(pin.upload_id),sha256:'a'.repeat(64)}});
 await expect(pending).resolves.toMatchObject({artifact:{sha256:'a'.repeat(64)}});
});
