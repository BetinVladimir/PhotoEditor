import {expect,it,vi} from 'vitest';
import {readInterchangeDownload} from './interchange-download';
import {OwnerAPI} from './owner-api';
import type {InterchangeJob,InterchangeInspection} from './interchange-job';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const now=Date.parse('2026-10-10T12:00:00Z');
const job=():InterchangeJob=>({id:id(1),workflow_id:id(1),tenant_id:id(2),project_id:id(3),revision:'2',direction:'export',format:'xmp',mapping_artifact_id:null,report_artifact_id:id(5),bundle_artifact_id:id(6)});
const inspection=():InterchangeInspection=>({schema_version:1,workflow_id:id(1),tenant_id:id(2),project_id:id(3),run_id:id(4),revision:'2',status:'completed',stage:'completed',ingest_run_id:null,collection_id:null,review:null});
const output=()=>({artifact_id:id(6),role:'bundle',sha256:'a'.repeat(64),bytes:4096,media_type:'application/zip',url:'https://objects.example/archive?signature=opaque',expires_at:new Date(now+60000).toISOString()});
it('owns an exact archive link and refuses unready or revoked jobs',()=>{
 const wire=output(),result=readInterchangeDownload({items:[wire]},job(),inspection(),now);wire.url='https://other.example';expect(result.url).toContain('objects.example');
 for(const patch of [{direction:'import'},{bundle_artifact_id:null,report_artifact_id:null}] as Partial<InterchangeJob>[])expect(()=>readInterchangeDownload({items:[output()]},{...job(),...patch},inspection(),now)).toThrow();
 expect(()=>readInterchangeDownload({items:[output()]},job(),{...inspection(),status:'running',stage:'finalizing'},now)).toThrow();
});
it('rejects aliases, duplicate receipts, malformed links and unsafe metadata',()=>{
 for(const items of [[],[output(),output()],[{...output(),artifact_id:id(9)}]])expect(()=>readInterchangeDownload({items},job(),inspection(),now)).toThrow();
 for(const patch of [{role:'document'},{media_type:'text/html'},{bytes:68719476737},{bytes:21},{bytes:1.5},{sha256:'bad'},{url:'javascript:alert(1)'},{url:'http://objects.example/archive'},{url:'https://user:secret@objects.example/file'},{url:'https://objects.example/file#fragment'},{expires_at:'invalid'},{expires_at:new Date(now+5000).toISOString()}])expect(()=>readInterchangeDownload({items:[{...output(),...patch}]},job(),inspection(),now)).toThrow();
 expect(readInterchangeDownload({items:[{...output(),url:'http://127.0.0.1:9000/archive'}]},job(),inspection(),now).url).toBe('http://127.0.0.1:9000/archive');
});
it('fetches fresh outputs and rechecks job authority before returning the URL',async()=>{
 const api=new OwnerAPI({request:vi.fn()} as any,id(2));
 const snapshot=vi.spyOn(api,'interchangeSnapshot').mockResolvedValue({job:job(),inspection:inspection()});
 vi.spyOn(api,'runOutputs').mockResolvedValue({items:[{...output(),expires_at:new Date(Date.now()+60000).toISOString()}]});
 expect((await api.interchangeDownload(id(3),id(1))).artifact_id).toBe(id(6));expect(snapshot).toHaveBeenCalledTimes(2);
});
it('refuses revision changes, revoked bundle visibility and cancellation after outputs',async()=>{
 for(const mode of ['revision','revoked','abort']){
  const controller=new AbortController(),api=new OwnerAPI({request:vi.fn()} as any,id(2));
  vi.spyOn(api,'interchangeSnapshot').mockResolvedValueOnce({job:job(),inspection:inspection()}).mockResolvedValueOnce(mode==='revision'?{job:{...job(),revision:'3'},inspection:{...inspection(),revision:'3'}}:{job:{...job(),report_artifact_id:null,bundle_artifact_id:null},inspection:inspection()});
  vi.spyOn(api,'runOutputs').mockImplementation(async()=>{if(mode==='abort')controller.abort();return {items:[output()]};});
  await expect(api.interchangeDownload(id(3),id(1),controller.signal)).rejects.toThrow();
 }
});
it('does not request output URLs for non-completed jobs',async()=>{
 const api=new OwnerAPI({request:vi.fn()} as any,id(2));vi.spyOn(api,'interchangeSnapshot').mockResolvedValue({job:job(),inspection:{...inspection(),status:'running',stage:'finalizing'}});
 const outputs=vi.spyOn(api,'runOutputs');await expect(api.interchangeDownload(id(3),id(1))).rejects.toThrow();expect(outputs).not.toHaveBeenCalled();
});
