import {expect,it} from 'vitest';
import {assertArchiveHandoffAttempt} from './interchange-archive-handoff';
import type {InterchangeArchiveAttempt} from './interchange-archive-attempt-store';
const attempt=():InterchangeArchiveAttempt=>({version:1,key:'original-key',body:{filename:'archive.zip',bytes:22,sha256:'a'.repeat(64)},checkpoint:{stage:'queued',parts:[{part_no:1,etag:'original'}],receipt:{upload_id:'00000000-0000-4000-8000-000000000001',state:'uploading',part_bytes:16777216,parts_total:1,request_sha256:'b'.repeat(64)}}});
it('allows an unchanged owned durable attempt',()=>{
 expect(()=>assertArchiveHandoffAttempt(attempt(),attempt())).not.toThrow();
});
it('refuses deleted, replaced, aborted or changed attempts after the ready read',()=>{
 const before=attempt();
 for(const mutate of [
  (v:InterchangeArchiveAttempt)=>{v.key='replacement';},
  (v:InterchangeArchiveAttempt)=>{v.checkpoint=null;},
  (v:InterchangeArchiveAttempt)=>{v.checkpoint!.stage='aborting';},
  (v:InterchangeArchiveAttempt)=>{v.checkpoint!.receipt.upload_id='00000000-0000-4000-8000-000000000002';},
  (v:InterchangeArchiveAttempt)=>{v.body.sha256='c'.repeat(64);},
  (v:InterchangeArchiveAttempt)=>{v.checkpoint!.parts[0].etag='replacement';},
 ]){const after=attempt();mutate(after);expect(()=>assertArchiveHandoffAttempt(before,after)).toThrow('INTERCHANGE_ARCHIVE_JOURNAL_CONFLICT');}
 expect(()=>assertArchiveHandoffAttempt(before,null)).toThrow();
 const aborted=attempt();aborted.checkpoint!.stage='aborting';expect(()=>assertArchiveHandoffAttempt(aborted,aborted)).toThrow();
});
