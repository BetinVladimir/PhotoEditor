import {expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {readExportBatchStatus} from './export-batch-status';
import {freezeExportBatchRequest,type ExportBatchTicket} from './export-batch-attempt-store';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const accepted=():ExportBatchTicket=>({batch_id:id(1),project_id:id(2),state:'queued',groups:[{key:id(3),package_id:id(4),metadata_policy:'strip',presets:[id(5)],ticket:{run_id:id(6),workflow_id:id(7),state:'queued'},failure_code:null}]});
const status=(run_status='running',state='running')=>{const ticket=accepted();return {...ticket,state,groups:ticket.groups.map(g=>({...g,run_status}))};};
it('owns the full current status and requires all selected presets to complete',()=>{
 const value=status('completed','completed'),owned=readExportBatchStatus(value,accepted());value.groups[0].presets[0]=id(20);expect(owned.groups[0].presets).toEqual([id(5)]);
 expect(readExportBatchStatus(status('completed_with_errors','failed'),accepted()).state).toBe('failed');
 expect(()=>readExportBatchStatus(status('completed_with_errors','completed'),accepted())).toThrow();
});
it('refuses missing, foreign and validly shaped but changed group authority',()=>{
 const base=status();for(const raw of [{batch_id:id(1),state:'running',groups:[{key:id(3),run_status:'running'}]},{...base,project_id:id(20)},{...base,extra:true},{...base,groups:[{...base.groups[0],package_id:id(20)}]},{...base,groups:[{...base.groups[0],ticket:{run_id:id(20),workflow_id:id(7),state:'queued'}}]},{...base,groups:[{...base.groups[0],run_status:'pending'}]}])expect(()=>readExportBatchStatus(raw,accepted())).toThrow();
});
it('permits a previously pending group to acquire its same immutable package ticket',()=>{
 const before=accepted();before.state='partial';before.groups[0].ticket=null;
 expect(readExportBatchStatus(status(),before).groups[0].ticket).toEqual(accepted().groups[0].ticket);
});
it('freezes export consent and selected presets before actual transport',async()=>{
 const body={snapshot_id:id(8),presets:[id(5)],approved_fields:[] as string[]},expected=structuredClone(body);
 const request=vi.fn().mockImplementation(async(_path,options)=>{body.presets[0]=id(20);body.approved_fields.push('gps');expect(options.body).toEqual(expected);return accepted()});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));await expect(api.createExportBatch(body,id(10))).resolves.toEqual(accepted());
 expect(()=>freezeExportBatchRequest({...expected,approved_fields:['unknown']})).toThrow();
});
it('does not expose a late accepted export after cancellation or dispatch a pre-cancelled status read',async()=>{
 const abort=new AbortController(),request=vi.fn().mockImplementation(async()=>{abort.abort();return accepted()});
 const api=new OwnerAPI({request} as unknown as ApiClient,id(9));
 await expect(api.createExportBatch({snapshot_id:id(8),presets:[id(5)]},id(10),abort.signal)).rejects.toMatchObject({name:'AbortError'});
 await expect(api.exportBatchStatus(id(1),abort.signal)).rejects.toMatchObject({name:'AbortError'});expect(request).toHaveBeenCalledTimes(1);
});
