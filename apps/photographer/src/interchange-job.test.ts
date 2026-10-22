import {expect,it,vi} from 'vitest';
import {assertInterchangeSnapshot,readInterchangeJob,readInterchangeInspection} from './interchange-job';
import {OwnerAPI} from './owner-api';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const inspection=()=>({schema_version:1 as const,workflow_id:id(1),tenant_id:id(2),project_id:id(3),run_id:id(4),revision:'2',status:'running' as const,stage:'parsing' as const,ingest_run_id:null,collection_id:null,review:null});
it('binds runtime inspection to current scope and consistent states',()=>{
 const wire=inspection(),owned=readInterchangeInspection(wire,id(2),id(3),id(1));wire.run_id=id(9);expect(owned.run_id).toBe(id(4));
 for(const patch of [{tenant_id:id(9)},{project_id:id(9)},{workflow_id:id(9)},{revision:'9007199254740992'},{status:'completed',stage:'parsing'},{stage:'unknown'}])expect(()=>readInterchangeInspection({...inspection(),...patch},id(2),id(3),id(1))).toThrow();
 const missingReview={...inspection()} as Record<string,unknown>;delete missingReview.review;
 expect(()=>readInterchangeInspection(missingReview,id(2),id(3),id(1))).toThrow('INTERCHANGE_INSPECTION_INVALID');
 for(const review of [undefined,false,0,'invalid',[]])expect(()=>readInterchangeInspection({...inspection(),review},id(2),id(3),id(1))).toThrow();
 expect(()=>readInterchangeInspection({...inspection(),unexpected:true},id(2),id(3),id(1))).toThrow('INTERCHANGE_INSPECTION_INVALID');
});
it('accepts pending null pointers without inventing report readiness',()=>{
 const value={id:id(1),revision:'2',document:null,data:{resource_type:'interchange_jobs',id:id(1),revision:'2',tenant_id:id(2),project_id:id(3),workflow_id:id(1),created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:01Z',direction:'import',format:'photoeditor_bundle',mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null}};
 expect(readInterchangeJob(value,id(2),id(3),id(1)).report_artifact_id).toBeNull();
 for(const data of [{...value.data,created_at:undefined},{...value.data,updated_at:'2025-12-31T23:59:59Z'},{...value.data,created_at:'2026-02-30T00:00:00Z'},{...value.data,created_at:'2026-01-01T24:00:00Z'},{...value.data,unexpected:true}])expect(()=>readInterchangeJob({...value,data},id(2),id(3),id(1))).toThrow('INTERCHANGE_JOB_INVALID');
 for(const document of [undefined,{artifact_id:id(8),sha256:'not-a-sha'},{artifact_id:id(8),sha256:'a'.repeat(64),unexpected:true}])expect(()=>readInterchangeJob({...value,document},id(2),id(3),id(1))).toThrow('INTERCHANGE_JOB_INVALID');
 expect(readInterchangeJob({...value,document:{artifact_id:id(8),sha256:'a'.repeat(64)}},id(2),id(3),id(1)).format).toBe('photoeditor_bundle');
});
it('validates scope and cancellation before request',async()=>{
 const request=vi.fn();const api=new OwnerAPI({request} as any,id(2));
 await expect(api.inspectInterchange('invalid',id(1))).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 const controller=new AbortController();controller.abort();await expect(api.inspectInterchange(id(3),id(1),controller.signal)).rejects.toThrow();expect(request).not.toHaveBeenCalled();
});

it('retries mismatched revisions and returns only a consistent snapshot',async()=>{
 let reads=0;
 const request=vi.fn((path:string)=>{
  if(path.endsWith('/inspection'))return Promise.resolve(inspection());
  reads++;
  return Promise.resolve({id:id(1),revision:reads===1?'1':'2',document:null,data:{resource_type:'interchange_jobs',id:id(1),revision:reads===1?'1':'2',tenant_id:id(2),project_id:id(3),workflow_id:id(1),created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:01Z',direction:'import',format:'photoeditor_bundle',mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null}});
 });
 const result=await new OwnerAPI({request} as any,id(2)).interchangeSnapshot(id(3),id(1));
 expect(result.job.revision).toBe(result.inspection.revision);expect(request).toHaveBeenCalledTimes(4);
});
it('bounds revision retries without exposing a mixed snapshot',async()=>{
 const request=vi.fn((path:string)=>Promise.resolve(path.endsWith('/inspection')?inspection():{id:id(1),revision:'1',document:null,data:{resource_type:'interchange_jobs',id:id(1),revision:'1',tenant_id:id(2),project_id:id(3),workflow_id:id(1),created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:01Z',direction:'import',format:'photoeditor_bundle',mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null}}));
 await expect(new OwnerAPI({request} as any,id(2)).interchangeSnapshot(id(3),id(1))).rejects.toThrow('INTERCHANGE_REVISION_CHANGED');
 expect(request).toHaveBeenCalledTimes(6);
});

it('validates and owns bounded proposed review targets',()=>{
 const target={entry_id:id(10),external_id:'photo-1',asset_version_id:id(11),state:'proposed',fields:[{name:'recipe',status:'exact',reason:null}]};
 const wire={...inspection(),status:'waiting_review',stage:'review',review:{report:{artifact_id:id(12),sha256:'a'.repeat(64)},proposed_count:1,entry_count:1,can_close:false,can_decide:true,targets:[target]}};
 const owned=readInterchangeInspection(wire,id(2),id(3),id(1));target.fields[0].name='changed';expect(owned.review!.targets![0].fields[0].name).toBe('recipe');
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,targets:[target,target]}},id(2),id(3),id(1))).toThrow();
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,can_close:true}},id(2),id(3),id(1))).toThrow();
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,unexpected:true}},id(2),id(3),id(1))).toThrow('INTERCHANGE_REVIEW_INVALID');
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,can_decide:undefined}},id(2),id(3),id(1))).toThrow('INTERCHANGE_REVIEW_INVALID');
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,targets:undefined}},id(2),id(3),id(1))).toThrow('INTERCHANGE_REVIEW_TARGETS_INVALID');
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,targets:[{...target,unexpected:true}]}},id(2),id(3),id(1))).toThrow('INTERCHANGE_REVIEW_TARGETS_INVALID');
 expect(()=>readInterchangeInspection({...wire,review:{...wire.review,targets:[{...target,fields:[{name:'recipe',status:'exact',reason:null,unexpected:true}]}]}},id(2),id(3),id(1))).toThrow('INTERCHANGE_REVIEW_TARGETS_INVALID');
});

it('sends only scoped proposed decisions and checks exact remaining count',async()=>{
 const wire={...inspection(),status:'waiting_review',stage:'review',review:{report:{artifact_id:id(12),sha256:'a'.repeat(64)},proposed_count:1,entry_count:1,can_close:false,can_decide:true,targets:[{entry_id:id(10),external_id:'photo',asset_version_id:id(11),state:'proposed',fields:[]}]}};
 const owned=readInterchangeInspection(wire,id(2),id(3),id(1));
 const request=vi.fn().mockResolvedValue({workflow_id:id(1),revision:'3',remaining:0,batch_id:id(15),child:null});
 const api=new OwnerAPI({request} as any,id(2));
 await expect(api.decideInterchangeReview(id(3),owned,[{entry_id:id(99),decision:'approve'}],'decide-1')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
 await expect(api.decideInterchangeReview(id(3),owned,[{entry_id:id(10),decision:'reject'}],'decide-1')).resolves.toMatchObject({remaining:0,child:null});
 expect(request.mock.calls[0][1]).toMatchObject({method:'POST',idempotencyKey:'decide-1',body:{expected_revision:'2',report_sha:'a'.repeat(64),decisions:[{entry_id:id(10),decision:'reject'}]}});
 request.mockResolvedValue({workflow_id:id(1),revision:'3',remaining:1,batch_id:id(15),child:null});
 await expect(api.decideInterchangeReview(id(3),owned,[{entry_id:id(10),decision:'reject'}],'decide-1')).rejects.toThrow();
});

it('accepts export stages and rejects mixed import state',()=>{
 for(const stage of ['exporting','finalizing']){
  expect(readInterchangeInspection({...inspection(),stage},id(2),id(3),id(1)).stage).toBe(stage);
  for(const patch of [{ingest_run_id:id(8)},{collection_id:id(9)},{status:'completed'}]){
   expect(()=>readInterchangeInspection({...inspection(),stage,...patch},id(2),id(3),id(1))).toThrow();
  }
 }
});

it('binds job direction, available outputs and inspection stage',()=>{
 const job={id:id(1),tenant_id:id(2),project_id:id(3),workflow_id:id(1),revision:'2',direction:'export' as const,format:'xmp' as const,mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null};
 const exporting={...inspection(),stage:'exporting' as const};
 expect(()=>assertInterchangeSnapshot(job,exporting)).not.toThrow();
 const ready={...job,report_artifact_id:id(7),bundle_artifact_id:id(8)};
 expect(()=>assertInterchangeSnapshot(ready,{...exporting,stage:'finalizing'})).not.toThrow();
 for(const [resource,status] of [[job,{...exporting,stage:'finalizing'}],[ready,exporting],[job,inspection()],[{...job,direction:'import'},exporting],[ready,{...exporting,stage:'finalizing',collection_id:id(9)}],[{...ready,report_artifact_id:null},{...exporting,stage:'finalizing'}]] as const){
  expect(()=>assertInterchangeSnapshot(resource as any,status)).toThrow('INTERCHANGE_SNAPSHOT_INVALID');
 }
 // Output access may be revoked after completion; status must stay completed.
 expect(()=>assertInterchangeSnapshot(job,{...exporting,status:'completed',stage:'completed'})).not.toThrow();
});
it('rejects same-revision export/import projection mix at API boundary',async()=>{
 const value={id:id(1),revision:'2',document:null,data:{resource_type:'interchange_jobs',id:id(1),revision:'2',tenant_id:id(2),project_id:id(3),workflow_id:id(1),created_at:'2026-01-01T00:00:00Z',updated_at:'2026-01-01T00:00:01Z',direction:'export',format:'xmp',mapping_artifact_id:null,report_artifact_id:null,bundle_artifact_id:null}};
 const request=vi.fn((path:string)=>Promise.resolve(path.endsWith('/inspection')?inspection():value));
 await expect(new OwnerAPI({request} as any,id(2)).interchangeSnapshot(id(3),id(1))).rejects.toThrow('INTERCHANGE_SNAPSHOT_INVALID');
});
