import {describe,it,expect} from 'vitest';
import {freezeDeliveryCreate,readDeliveryCreateTicket,type DeliveryCreateRequest} from './delivery-create';
const id=(n:number)=>`00000000-0000-4000-8000-${n.toString().padStart(12,'0')}`;
const request=():DeliveryCreateRequest=>({project_id:id(1),snapshot_id:id(2),name:'Delivery',policy:{required_presets:[id(3)],optional_presets:[],require_review:true,metadata_policy:'strip',provenance:'off',allow_omitted:false}});
describe('delivery creation contract',()=>{
 it('owns intent arrays and accepts Unicode codepoint name limits',()=>{
  const source=request();source.name='📷'.repeat(200);const frozen=freezeDeliveryCreate(source,'key');source.policy.required_presets[0]=id(4);expect(frozen.policy.required_presets).toEqual([id(3)]);
 });
 it('rejects duplicate presets, unknown fields and invalid keys before IO',()=>{
  const duplicate=request();duplicate.policy.optional_presets=[id(3)];expect(()=>freezeDeliveryCreate(duplicate,'key')).toThrow();
  for(const key of ['', 'has space','x'.repeat(129),'\n'])expect(()=>freezeDeliveryCreate(request(),key)).toThrow();
  expect(()=>freezeDeliveryCreate({...request(),extra:true} as DeliveryCreateRequest,'key')).toThrow();
  const malformed=request();malformed.policy.optional_presets=null as unknown as string[];expect(()=>freezeDeliveryCreate(malformed,'key')).toThrow();
 });
 it('accepts sixteen presets in either group but rejects seventeen in total',()=>{
  const allRequired=request();allRequired.policy.required_presets=Array.from({length:16},(_,i)=>id(i+10));
  expect(freezeDeliveryCreate(allRequired,'sixteen').policy.required_presets).toHaveLength(16);
  const mixed=request();mixed.policy.required_presets=allRequired.policy.required_presets.slice(0,15);mixed.policy.optional_presets=[allRequired.policy.required_presets[15]];
  expect(freezeDeliveryCreate(mixed,'mixed').policy.optional_presets).toHaveLength(1);
  mixed.policy.optional_presets.push(id(30));
  expect(()=>freezeDeliveryCreate(mixed,'seventeen')).toThrow();
 });
 it('accepts only exact queued tickets',()=>{
  expect(readDeliveryCreateTicket({run_id:id(4),workflow_id:id(5),state:'queued'}).run_id).toBe(id(4));
  for(const value of [{run_id:id(4),workflow_id:id(5),state:'running'},{run_id:id(4),workflow_id:id(5),state:'queued',extra:true},{run_id:'invalid',workflow_id:id(5),state:'queued'},null])expect(()=>readDeliveryCreateTicket(value)).toThrow();
 });
});

it('pins sorted explicit metadata consent without borrowing arrays',()=>{
 const source=request();source.policy.metadata_policy='approved_exif';source.policy.approved_fields=['gps','author'];
 const frozen=freezeDeliveryCreate(source,'metadata');source.policy.approved_fields[0]='lens';
 expect(frozen.policy.approved_fields).toEqual(['author','gps']);
 const empty=request();empty.policy.approved_fields=[];expect(freezeDeliveryCreate(empty,'key').policy).not.toHaveProperty('approved_fields');
});
it('refuses implicit, duplicate, null and unknown metadata consent',()=>{
 for(const fields of [['gps'],['gps','gps'],['private_serial'],null]){
  const source=request();if(fields===null||fields.length!==1||fields[0]!=='gps')source.policy.metadata_policy='approved_exif';
  source.policy.approved_fields=fields as unknown as NonNullable<DeliveryCreateRequest['policy']['approved_fields']>;
  expect(()=>freezeDeliveryCreate(source,'key')).toThrow();
 }
});

it('pins only an explicit required signing policy choice',()=>{
 const required=request();required.policy.provenance='required';required.policy.signing_policy_id=id(9);required.policy.signing_policy_sha='a'.repeat(64);
 const frozen=freezeDeliveryCreate(required,'signed');expect(frozen.policy.signing_policy_id).toBe(id(9));
 required.policy.signing_policy_id=id(10);expect(frozen.policy.signing_policy_id).toBe(id(9));
 for(const changed of [
  {...request(),policy:{...request().policy,signing_policy_id:id(9)}},
  {...request(),policy:{...request().policy,signing_policy_id:null}},
  {...request(),policy:{...request().policy,provenance:'required',signing_policy_id:id(9)}},
  {...request(),policy:{...request().policy,provenance:'required',signing_policy_id:id(9),signing_policy_sha:'bad'}},
 ])expect(()=>freezeDeliveryCreate(changed as DeliveryCreateRequest,'signed')).toThrow();
});
