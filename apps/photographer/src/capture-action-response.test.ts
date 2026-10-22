import {describe,it,expect,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import type {CaptureAlert} from './capture-types';
import {OwnerAPI} from './owner-api';
import {freezeCaptureAcknowledgement,readCaptureAcknowledgement,readCaptureConfirmation} from './capture-action-response';
const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const tenant=id(1),receipt=id(2),alertID=id(3),session=id(4),actor=id(5);
const alert=():CaptureAlert=>({id:alertID,session_id:session,asset_version_id:id(6),report_artifact_id:id(7),receipt_id:receipt,confirmation_id:null,revision:'9007199254740993',severity:'warning',qa_stale:false,created_at:'2026-10-17T10:00:00+00:00',acknowledged_by:null,acknowledged_at:null});
function acknowledged(){const {confirmation_id,...row}=alert();return {...row,resource_type:'capture_alerts',tenant_id:tenant,project_id:id(8),updated_at:'2026-10-17T10:01:00+00:00',revision:'9007199254740994',acknowledged_by:actor,acknowledged_at:'2026-10-17T10:01:00+00:00'};}
const confirmation=()=>({confirmation_id:id(9),receipt_id:receipt,alert_id:alertID});
describe('Capture acknowledged and confirmed wire boundaries (authored, not executed)',()=>{
 it('accepts actual sanitized SQL acknowledgement without inventing the list-only confirmation join',()=>{
  const raw=acknowledged(),value=readCaptureAcknowledgement(raw,tenant,freezeCaptureAcknowledgement(alert()));expect(value).toEqual(raw);expect(value).not.toBe(raw);expect(value.confirmation_id).toBeUndefined();
 });
 it('refuses mismatched or malformed acknowledgement and exact revision drift',()=>{
  for(const patch of [{tenant_id:id(30)},{id:id(30)},{session_id:id(30)},{asset_version_id:id(30)},{receipt_id:null},{report_artifact_id:id(30)},{severity:'info'},{qa_stale:true},{created_at:'2026-10-17T09:00:00Z'},{updated_at:'1'},{revision:'9007199254740993'},{revision:'9007199254740995'},{acknowledged_by:null},{acknowledged_at:null},{technical_quality:.9},{confirmation_id:id(30)}])expect(()=>readCaptureAcknowledgement({...acknowledged(),...patch},tenant,alert())).toThrow();
 });
 it('accepts only the exact selected receipt/alert confirmation with a nonzero confirmation ID',()=>{
  expect(readCaptureConfirmation(confirmation(),receipt,alertID)).toEqual(confirmation());
  for(const value of [null,[],{...confirmation(),confirmation_id:'bad'},{...confirmation(),confirmation_id:'00000000-0000-0000-0000-000000000000'},{...confirmation(),receipt_id:id(30)},{...confirmation(),alert_id:id(30)},{...confirmation(),extra:true}])expect(()=>readCaptureConfirmation(value,receipt,alertID)).toThrow();
 });
 it('freezes acknowledgement body and binding before transport and retains caller key',async()=>{
  let finish!:(v:unknown)=>void;const request=vi.fn(()=>new Promise(resolve=>finish=resolve)),api=new OwnerAPI({request} as unknown as ApiClient,tenant),chosen=alert();
  const pending=api.acknowledgeCaptureAlert(chosen,'same-key');chosen.id=id(30);chosen.revision='1';chosen.asset_version_id=id(31);finish(acknowledged());
  await expect(pending).resolves.toEqual(acknowledged());expect(request.mock.calls[0]).toEqual([`/api/v1/tenants/${tenant}/capture-alerts/${alertID}/ack`,expect.objectContaining({body:{expected_revision:'9007199254740993'},idempotencyKey:'same-key'})]);
 });
 it('rejects invalid scope and pre-cancellation before IO, and ignores late acknowledgement/confirmation after cancellation',async()=>{
  const request=vi.fn().mockResolvedValue(confirmation()),api=new OwnerAPI({request} as unknown as ApiClient,tenant),cancelled=new AbortController();cancelled.abort();
  await expect(api.confirmCaptureFrame(receipt,alertID,'key',cancelled.signal)).rejects.toThrow();await expect(api.confirmCaptureFrame('receipt/id',alertID,'key')).rejects.toThrow();await expect(api.acknowledgeCaptureAlert({...alert(),revision:'01'},'key')).rejects.toThrow();expect(request).not.toHaveBeenCalled();
  for(const kind of ['ack','confirm']){let finish!:(v:unknown)=>void;request.mockImplementationOnce(()=>new Promise(resolve=>finish=resolve));const controller=new AbortController();const pending=kind==='ack'?api.acknowledgeCaptureAlert(alert(),'key',controller.signal):api.confirmCaptureFrame(receipt,alertID,'key',controller.signal);controller.abort();finish(kind==='ack'?acknowledged():confirmation());await expect(pending).rejects.toThrow();}
 });
});
