import {expect,it} from 'vitest';
import {archiveArtifactID,archiveUploadID,archiveRequestSHA,freezeArchiveUpload,readArchiveUploadReceipt,readArchiveStatus,readArchiveAccepted,readArchiveAbortAccepted,readArchivePartURLs,archiveCompletionParts} from './interchange-archive-upload';
const project='00000000-0000-4000-8000-000000000001';
const upload='00000000-0000-4000-8000-000000000002';
const request=()=>({filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)});
const receipt=()=>({upload_id:upload,state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:archiveRequestSHA(project,request())});
it('owns request and validates exact hash and part geometry',()=>{
 const input=request(),owned=freezeArchiveUpload(input,'key');input.bytes=23;expect(owned).toEqual(request());
 expect(readArchiveUploadReceipt(receipt(),project,owned)).toEqual(receipt());
 for(const patch of [{request_sha256:'b'.repeat(64)},{parts_total:2},{part_bytes:1},{state:'reserved'},{upload_id:project.toUpperCase()+'x'},{extra:true}])expect(()=>readArchiveUploadReceipt({...receipt(),...patch},project,owned)).toThrow();
});
it('rejects unsafe names, sizes, keys and unknown fields',()=>{
 for(const patch of [{filename:'../a.zip'},{filename:'a\u0085.zip'},{bytes:21},{bytes:64*1024**3+1},{sha256:'A'.repeat(64)},{extra:true}])expect(()=>freezeArchiveUpload({...request(),...patch},'key')).toThrow();
 expect(()=>freezeArchiveUpload(request(),'bad key')).toThrow();
 expect(archiveRequestSHA(project,request())).not.toBe(archiveRequestSHA(upload,request()));
});

it('separates queued acknowledgement from independently ready artifact',()=>{
 const pin=readArchiveUploadReceipt(receipt(),project,request());
 expect(readArchiveAccepted({upload_id:upload,accepted:true},pin)).toEqual({upload_id:upload,accepted:true});
 const status={upload_id:upload,state:'verified',request_sha256:pin.request_sha256,expires_at:'2026-10-11T10:00:00Z',ready:false};
 expect(readArchiveStatus(status,pin,request()).artifact).toBeUndefined();
 expect(readArchiveStatus({...status,state:'reserved',begin_recovery:'reconciliation_required'},pin,request()).begin_recovery).toBe('reconciliation_required');
 expect(()=>readArchiveStatus({...status,begin_recovery:'reconciliation_required'},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,state:'reserved',begin_recovery:'unknown'},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,state:'reserved',begin_recovery:'resolved'},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,error_code:'UPLOAD_ABORTED'},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,state:'aborting'},pin,request())).toThrow();
 expect(readArchiveStatus({...status,state:'aborting',error_code:'UPLOAD_ABORTED'},pin,request()).error_code).toBe('UPLOAD_ABORTED');
 expect(()=>readArchiveStatus({...status,ready:true},pin,request())).toThrow();
 expect(()=>readArchiveStatus({...status,ready:true,artifact:{artifact_id:project,sha256:'b'.repeat(64)}},pin,request())).toThrow();
 expect(readArchiveStatus({...status,ready:true,artifact:{artifact_id:archiveArtifactID(upload),sha256:request().sha256}},pin,request()).artifact?.sha256).toBe(request().sha256);
});
it('derives a saved archive attempt ID from exact tenant, actor and key',()=>{
 const first=archiveUploadID(project,upload,'saved-key');
 expect(first).toMatch(/^[0-9a-f-]{36}$/);
 expect(archiveUploadID(project,upload,'saved-key')).toBe(first);
 expect(archiveUploadID(project,upload,'other-key')).not.toBe(first);
 expect(()=>archiveUploadID(project,upload,'bad key')).toThrow();
});
it('owns complete receipts and rejects duplicate or unsafe signed parts',()=>{
 const pin=readArchiveUploadReceipt(receipt(),project,request()),parts=[{part_no:1,etag:'receipt'}];
 const owned=archiveCompletionParts(pin,parts);parts[0].etag='changed';expect(owned[0].etag).toBe('receipt');
 for(const etag of ['bad\t','bad\0','bad\x7f'])expect(()=>archiveCompletionParts(pin,[{part_no:1,etag}])).toThrow();
 const now=Date.parse('2026-10-11T10:00:00Z'),url={part_no:1,url:'https://storage.example/signed',expires_at:'2026-10-11T10:05:00Z'};
 expect(readArchivePartURLs({parts:[url]},pin,[1],now)[0].part_no).toBe(1);
 for(const patch of [{part_no:2},{url:'javascript:alert(1)'},{url:'http://storage.example/signed'},{expires_at:'2026-10-11T09:00:00Z'},{expires_at:'2026-10-11 10:05:00Z'},{expires_at:'2026-10-11T10:05:00'}])expect(()=>readArchivePartURLs({parts:[{...url,...patch}]},pin,[1],now)).toThrow();
 expect(readArchivePartURLs({parts:[{...url,url:'http://127.0.0.1:9443/signed'}]},pin,[1],now)).toHaveLength(1);
 expect(readArchivePartURLs({parts:[{...url,url:'http://127.0.0.2:9443/signed'}]},pin,[1],now)).toHaveLength(1);
});

it('requires explicit cleanup-pending abort acknowledgement for the same upload',()=>{
 const pin=readArchiveUploadReceipt(receipt(),project,request());
 expect(readArchiveAbortAccepted({upload_id:upload,accepted:true,cleanup_pending:true},pin).cleanup_pending).toBe(true);
 expect(readArchiveAbortAccepted({upload_id:upload,accepted:true,cleanup_pending:false},pin).cleanup_pending).toBe(false);
 for(const value of [{upload_id:upload,accepted:true},{upload_id:upload,accepted:true,cleanup_pending:'false'},{upload_id:project,accepted:true,cleanup_pending:true}])expect(()=>readArchiveAbortAccepted(value,pin)).toThrow();
});

it('matches server signed URL byte and lifetime limits without browser normalization bypasses',()=>{
 const pin=readArchiveUploadReceipt(receipt(),project,request()),now=Date.parse('2026-10-11T10:00:00Z');
 const link={part_no:1,url:'https://storage.example/signed',expires_at:new Date(now+15*60*1000).toISOString()};
 expect(readArchivePartURLs({parts:[link]},pin,[1],now)).toHaveLength(1);
 const prefix='https://storage.example/';
 expect(readArchivePartURLs({parts:[{...link,url:prefix+'a'.repeat(32768-prefix.length)}]},pin,[1],now)).toHaveLength(1);
 for(const patch of [
  {expires_at:new Date(now+15*60*1000+1).toISOString()},
  {expires_at:new Date(now+5000).toISOString()},
  {url:prefix+'a'.repeat(32769-prefix.length)},
  {url:prefix+'é'.repeat(17000)},
  {url:'https://storage.example/\nnormalized'},
  {url:'https://storage.example/\rnormalized'},
  {url:'https://storage.example/\x00normalized'},
  {url:'https://@storage.example/signed'},
  {url:'https:storage.example/signed'},
 ])expect(()=>readArchivePartURLs({parts:[{...link,...patch}]},pin,[1],now)).toThrow('INTERCHANGE_ARCHIVE_UPLOAD_INVALID');
});

it('binds ready artifact UUID to exact upload namespace even when content SHA matches',()=>{
 const pin=readArchiveUploadReceipt(receipt(),project,request());
 const status={upload_id:upload,state:'verified',request_sha256:pin.request_sha256,expires_at:'2030-01-01T00:00:00Z',ready:true,artifact:{artifact_id:project,sha256:request().sha256}};
 expect(()=>readArchiveStatus(status,pin,request())).toThrow();
 expect(archiveArtifactID(upload)).not.toBe(archiveArtifactID(project));
 expect(()=>archiveArtifactID('invalid')).toThrow();
});
it('matches the fixed archive UUIDv5 interoperability vector',()=>{expect(archiveArtifactID(upload)).toBe('44d0ac3c-5d7a-5a01-b987-7a64a0a3af30');});
it('matches an independently computed RFC 4122 SHA1 upload identifier in all five UUID groups',()=>{
 expect(archiveUploadID(project,upload,'original-key')).toBe('fddafd99-fb74-5adb-bd8d-815902f5df1a');
});
