import {expect,it} from 'vitest';
import {readContactSheetAccepted,freezeContactSheetAdmission} from './contact-sheet-response';
const id='11111111-1111-4111-8111-111111111111';
it('reads the immutable accepted receipt and detaches it from the caller object',()=>{
 const wire={status:'queued',run_id:id};const result=readContactSheetAccepted(wire);
 wire.run_id='22222222-2222-4222-8222-222222222222';
 expect(result).toEqual({run_id:id,status:'queued'});
});
it.each([null,[],{run_id:id},{run_id:id,status:'running'},{run_id:id,status:'succeeded'},{run_id:id,status:'queued',workflow_id:id},{run_id:'00000000-0000-0000-0000-000000000000',status:'queued'},{run_id:'invalid',status:'queued'}])('refuses incomplete, substituted or non-admission receipts',raw=>{
 expect(()=>readContactSheetAccepted(raw)).toThrow('CONTACT_SHEET_RECEIPT_INVALID');
});

it('validates the complete actual server layout range before transport',()=>{expect(freezeContactSheetAdmission(id,'bg',8,10)).toEqual({selection_snapshot_id:id,locale:'bg',columns:8,rows:10});for(const [columns,rows] of [[0,5],[9,5],[4,0],[4,11],[1.5,5]])expect(()=>freezeContactSheetAdmission(id,'en',columns,rows)).toThrow('REQUEST_INVALID');expect(()=>freezeContactSheetAdmission(id,'xx',4,5)).toThrow('REQUEST_INVALID');});
