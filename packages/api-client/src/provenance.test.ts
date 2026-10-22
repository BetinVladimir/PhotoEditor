import {expect,it,vi} from 'vitest';
import {freezeSignCreate,readSigningTicket,signExport,readSigningPolicies,signingPolicies} from './provenance';

const id=(n:number)=>`11111111-1111-4111-8111-${n.toString().padStart(12,'0')}`;
const ticket=()=>({run_id:id(4),workflow_id:id(5),state:'queued' as const});

it('loads redacted policy choices with an owned response and exact export scope',async()=>{
 const value={items:[{id:id(3),revision:'9223372036854775807'}]},request=vi.fn().mockResolvedValue(value),signal=new AbortController().signal;
 const result=await signingPolicies({request},id(1),id(2),signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id(1)}/exports/${id(2)}/signing-policies`,{signal});
 expect(result).toEqual(value.items);result[0].revision='2';expect(value.items[0].revision).toBe('9223372036854775807');
 expect(readSigningPolicies({items:[]})).toEqual([]);
});

it.each([null,{},[],{items:null},{items:[{id:id(3),revision:1}]},
 {items:[{id:id(3),revision:'0'}]},{items:[{id:id(3),revision:'01'}]},
 {items:[{id:id(3),revision:'9223372036854775808'}]},
 {items:[{id:id(3),revision:'1',key_handle:'private'}]},
 {items:[{id:id(3),revision:'1'},{id:id(3),revision:'2'}]},
 {items:Array.from({length:257},()=>({id:id(3),revision:'1'}))}])('rejects malformed, duplicated or expanded policy catalogues',value=>{
 expect(()=>readSigningPolicies(value)).toThrow('SIGNING_POLICIES_INVALID');
});

it('does not consume policy choices after cancellation',async()=>{
 const controller=new AbortController(),request=vi.fn().mockImplementation(async()=>{controller.abort();return {items:[]}});
 await expect(signingPolicies({request},id(1),id(2),controller.signal)).rejects.toThrow();
});

it('posts owned policy with caller retry key and returns an owned queued acknowledgement',async()=>{
 const body={signing_policy_id:id(3)},response=ticket(),request=vi.fn().mockResolvedValue(response),signal=new AbortController().signal;
 const result=await signExport({request},id(1),id(2),body,'stable-key',signal);
 expect(request).toHaveBeenCalledWith(`/api/v1/tenants/${id(1)}/exports/${id(2)}/provenance`,{method:'POST',body,idempotencyKey:'stable-key',signal});
 expect(request.mock.calls[0][1].body).not.toBe(body);
 expect(result).toEqual(response);expect(result).not.toBe(response);
});

it.each([{},null,{signing_policy_id:null},{signing_policy_id:'bad'},
 {signing_policy_id:'00000000-0000-0000-0000-000000000000'},
 {signing_policy_id:id(3),key_handle:'untrusted'}])('rejects an invalid/expanded policy request',value=>{
 expect(()=>freezeSignCreate(value as never)).toThrow('SIGNING_REQUEST_INVALID');
});

it.each([{},null,{...ticket(),state:'completed'},{...ticket(),workflow_id:id(4)},
 {...ticket(),run_id:'bad'},{...ticket(),signed_url:'https://untrusted.invalid'}])('rejects malformed or non-queued acknowledgements',value=>{
 expect(()=>readSigningTicket(value)).toThrow('SIGNING_TICKET_INVALID');
});

it('preserves the exact key and body across caller retries after an ambiguous failure',async()=>{
 const request=vi.fn().mockRejectedValueOnce(new Error('connection lost')).mockResolvedValueOnce(ticket());
 const body={signing_policy_id:id(3)};
 await expect(signExport({request},id(1),id(2),body,'same-key')).rejects.toThrow('connection lost');
 await signExport({request},id(1),id(2),body,'same-key');
 expect(request.mock.calls[0]).toEqual(request.mock.calls[1]);
});

it('checks cancellation before transport and after its response',async()=>{
 const controller=new AbortController();controller.abort();const request=vi.fn();
 await expect(signExport({request},id(1),id(2),{signing_policy_id:id(3)},'key',controller.signal)).rejects.toThrow();
 expect(request).not.toHaveBeenCalled();
 const late=new AbortController(),transport=vi.fn().mockImplementation(async()=>{late.abort();return ticket()});
 await expect(signExport({request:transport},id(1),id(2),{signing_policy_id:id(3)},'key',late.signal)).rejects.toThrow();
});

it.each(['','contains space','x'.repeat(129)])('rejects invalid retry keys without IO',async key=>{
 const request=vi.fn();await expect(signExport({request},id(1),id(2),{signing_policy_id:id(3)},key)).rejects.toThrow('SIGNING_SCOPE_INVALID');expect(request).not.toHaveBeenCalled();
});
