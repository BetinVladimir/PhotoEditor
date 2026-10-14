import {afterEach,expect,it,vi} from 'vitest';
import {executeArchiveUpload,type ArchiveCheckpoint} from './interchange-archive-executor';
import {archiveRequestSHA,type ArchiveUploadReceipt} from './interchange-archive-upload';
const project='00000000-0000-4000-8000-000000000001',upload='00000000-0000-4000-8000-000000000002';
function fixture(){
 const request={filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)},receipt:ArchiveUploadReceipt={upload_id:upload,state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request)};
 const events:string[]=[],saved:ArchiveCheckpoint[]=[];
 const source={name:request.filename,size:22,sha256:request.sha256,lastModified:0,read:vi.fn(async()=>{events.push('read');return new Uint8Array(22);})};
 const api={interchangeArchiveParts:vi.fn(async()=>[{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}]),completeInterchangeArchive:vi.fn(async()=>{events.push('complete');return{upload_id:upload,accepted:true};}),interchangeArchiveStatus:vi.fn(async()=>({upload_id:upload,state:'completing',request_sha256:receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false}))};
 const journal={save:vi.fn(async(c:ArchiveCheckpoint)=>{saved.push(c);events.push('save:'+c.stage);})};
 const put=vi.fn(async()=>{events.push('put');return 'etag';});
 return {request,receipt,events,saved,source,api,journal,put,project,signal:new AbortController().signal,checkpoint:{receipt,parts:[],stage:'uploading'} as ArchiveCheckpoint};
}
it('persists each part before completion and reports queued without false ready',async()=>{
 const f=fixture(),result=await executeArchiveUpload(f);expect(result.ready).toBe(false);
 expect(f.events).toEqual(['save:uploading','read','put','save:uploading','complete','save:queued']);expect(f.saved[1].parts).toEqual([{part_no:1,etag:'etag'}]);
});
it('resumes durable ETags without reading or uploading them again',async()=>{
 const f=fixture();f.checkpoint.parts=[{part_no:1,etag:'saved'}];await executeArchiveUpload(f);
 expect(f.source.read).not.toHaveBeenCalled();expect(f.put).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).toHaveBeenCalledOnce();
});
it('stops before completion when durable checkpoint fails',async()=>{
 const f=fixture();f.journal.save.mockRejectedValueOnce(new Error('storage failed'));
 await expect(executeArchiveUpload(f)).rejects.toThrow('storage failed');expect(f.put).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).not.toHaveBeenCalled();
});

it('never uploads or completes after durable abort intent',async()=>{
 const f=fixture();f.checkpoint.stage='aborting';await executeArchiveUpload(f);
 expect(f.source.read).not.toHaveBeenCalled();expect(f.put).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).not.toHaveBeenCalled();expect(f.api.interchangeArchiveStatus).toHaveBeenCalledOnce();
});

afterEach(()=>vi.restoreAllMocks());

it('refuses a signed part URL that expires while the local file is being read',async()=>{
 const f=fixture(),now=Date.now();vi.spyOn(Date,'now').mockReturnValue(now);
 f.source.read.mockImplementation(async()=>{vi.mocked(Date.now).mockReturnValue(now+300000);return new Uint8Array(22);});
 await expect(executeArchiveUpload(f)).rejects.toThrow('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
 expect(f.put).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).not.toHaveBeenCalled();expect(f.saved).toHaveLength(1);
});
it('refuses source metadata changes during a local read without sending bytes',async()=>{
 const f=fixture();f.source.read.mockImplementation(async()=>{f.source.lastModified=1;return new Uint8Array(22);});
 await expect(executeArchiveUpload(f)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');expect(f.put).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).not.toHaveBeenCalled();
});
it('uses only status when the durable completion acknowledgement already exists',async()=>{
 const f=fixture();f.checkpoint.stage='queued';f.checkpoint.parts=[{part_no:1,etag:'saved'}];await executeArchiveUpload(f);
 expect(f.api.interchangeArchiveStatus).toHaveBeenCalledOnce();expect(f.source.read).not.toHaveBeenCalled();expect(f.api.interchangeArchiveParts).not.toHaveBeenCalled();expect(f.api.completeInterchangeArchive).not.toHaveBeenCalled();expect(f.journal.save).not.toHaveBeenCalled();
});
it('does not query status when persisting the completion acknowledgement fails',async()=>{
 const f=fixture();f.journal.save.mockImplementation(async checkpoint=>{if(checkpoint.stage==='queued')throw new Error('journal full');});
 await expect(executeArchiveUpload(f)).rejects.toThrow('journal full');expect(f.api.completeInterchangeArchive).toHaveBeenCalledOnce();expect(f.api.interchangeArchiveStatus).not.toHaveBeenCalled();
 expect(f.checkpoint.stage).toBe('uploading');expect(f.checkpoint.parts).toEqual([]);
});


it.each(['parts','complete','status'] as const)('rejects request mutation by %s transport without publishing acceptance',async phase=>{
 const f=fixture();
 if(phase==='parts')f.api.interchangeArchiveParts.mockImplementation(async(...args:unknown[])=>{
  (args[1] as typeof f.request).sha256='b'.repeat(64);
  return [{part_no:1,url:'https://storage.example/signed',expires_at:new Date(Date.now()+300000).toISOString()}];
 });
 if(phase==='complete')f.api.completeInterchangeArchive.mockImplementation(async(...args:unknown[])=>{
  (args[1] as typeof f.request).sha256='b'.repeat(64);return {upload_id:upload,accepted:true};
 });
 if(phase==='status'){
  f.checkpoint.stage='queued';f.checkpoint.parts=[{part_no:1,etag:'saved'}];
  f.api.interchangeArchiveStatus.mockImplementation(async(...args:unknown[])=>{
   (args[1] as typeof f.request).sha256='b'.repeat(64);
   return {upload_id:upload,state:'completing',request_sha256:f.receipt.request_sha256,expires_at:new Date(Date.now()+3600000).toISOString(),ready:false};
  });
 }
 await expect(executeArchiveUpload(f)).rejects.toThrow('INTERCHANGE_ARCHIVE_REQUEST_CHANGED');
 expect(f.request.sha256).toBe('a'.repeat(64));expect(f.saved.some(c=>c.stage==='queued')).toBe(false);
 if(phase==='parts')expect(f.source.read).not.toHaveBeenCalled();
});
it('rejects changed completion ETags before durable queued acknowledgement',async()=>{
 const f=fixture();f.checkpoint.parts=[{part_no:1,etag:'saved'}];
 f.api.completeInterchangeArchive.mockImplementation(async(...args:unknown[])=>{
  (args[3] as {etag:string}[])[0].etag='substituted';return {upload_id:upload,accepted:true};
 });
 await expect(executeArchiveUpload(f)).rejects.toThrow('INTERCHANGE_ARCHIVE_COMPLETION_CHANGED');
 expect(f.checkpoint.parts).toEqual([{part_no:1,etag:'saved'}]);
 expect(f.saved.some(c=>c.stage==='queued')).toBe(false);expect(f.api.interchangeArchiveStatus).not.toHaveBeenCalled();
});
