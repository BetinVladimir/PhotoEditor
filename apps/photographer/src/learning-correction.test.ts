import {describe,expect,it,vi} from 'vitest';
import type {ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import {freezeLearningCorrection,type LearningCorrectionCreate} from './learning-correction';

const id=(n:number)=>`00000000-0000-4000-8000-${String(n).padStart(12,'0')}`;
const request=():LearningCorrectionCreate=>({
  client_event_id:id(1),session_id:id(2),asset_version_id:id(3),
  base_document_id:id(4),approved_document_id:id(5),reason:'retouch',
  learn:true,person_id:null,scene_group_id:null,consent_id:id(6),
  consent_epoch:'1',portrait_detail_producer:{
    run_id:id(7),task_id:id(8),result_id:id(9),attempt_id:id(10)}
});

describe('learning correction producer binding',()=>{
  it('owns the exact producer and preserves the supplied idempotency key',async()=>{
    const input=request();
    const transport=vi.fn().mockResolvedValue({
      id:id(11),project_id:id(12),client_event_id:id(1),asset_version_id:id(3)});
    const api=new OwnerAPI({request:transport} as unknown as ApiClient,id(13));
    const receipt=await api.recordCorrection(input,'correction-key');
    input.portrait_detail_producer!.run_id=id(20);
    expect(transport.mock.calls[0][0]).toBe(
      `/api/v1/tenants/${id(13)}/corrections`);
    expect(transport.mock.calls[0][1]).toEqual(expect.objectContaining({
      method:'POST',idempotencyKey:'correction-key',
      body:expect.objectContaining({portrait_detail_producer:{
        run_id:id(7),task_id:id(8),result_id:id(9),attempt_id:id(10)}})}));
    expect(receipt).toEqual({id:id(11),project_id:id(12),
      client_event_id:id(1),asset_version_id:id(3)});
  });
  it('refuses unknown, incomplete and non-retouch producer evidence before IO',()=>{
    const input=request();
    expect(()=>freezeLearningCorrection({...input,
      portrait_detail_producer:{...input.portrait_detail_producer!,attempt_id:''}}))
      .toThrow('INVALID_DETAIL_PRODUCER');
    expect(()=>freezeLearningCorrection({...input,reason:'exposure'}))
      .toThrow('INVALID_DETAIL_PRODUCER');
    expect(()=>freezeLearningCorrection({...input,unexpected:true} as never))
      .toThrow('INVALID_CORRECTION');
  });
});
