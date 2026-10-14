import { describe, expect, it } from "vitest";
import { ApiClient, EditorStore, SelectionStore, type AuthStore } from "./index";

const response = (status: number, body: unknown = {}) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
describe("ApiClient", () => {
  it('reads bounded authenticated PNG bytes and refreshes once',async()=>{
    let token='old',refreshes=0;
    const auth:AuthStore={principal:()=>({kind:'owner',accessToken:token}),refresh:async()=>{refreshes++;token='new';return token},clear:()=>{}};
    const fetcher:typeof fetch=async(_url,init)=>{
      const headers=new Headers(init?.headers);expect(headers.get('Accept')).toBe('image/png');
      if(headers.get('Authorization')==='Bearer old')return response(401);
      return new Response(new Uint8Array([137,80,78,71]),{headers:{'Content-Type':'image/png','Content-Length':'4'}});
    };
    const bytes=await new ApiClient('https://example.test',auth,fetcher).requestBytes('/api/v1/preview','image/png',8);
    expect([...bytes]).toEqual([137,80,78,71]);expect(refreshes).toBe(1);
  });
  it('rejects oversized and wrong-media binary responses',async()=>{
    const auth:AuthStore={principal:()=>({kind:'owner',accessToken:'token'}),refresh:async()=>'',clear:()=>{}};
    for(const contentType of ['text/html','image/png']){
      const fetcher:typeof fetch=async()=>new Response(new Uint8Array([1,2,3,4]),{headers:{'Content-Type':contentType}});
      await expect(new ApiClient('',auth,fetcher).requestBytes('/api/v1/preview','image/png',contentType==='image/png'?3:8)).rejects.toThrow('BINARY_RESPONSE_INVALID');
    }
  });
  it("single-flights refresh and retries each owner request once", async () => {
    let token="old",refreshes=0,calls=0; const auth:AuthStore={principal:()=>({kind:"owner",accessToken:token}),refresh:async()=>{refreshes++;await Promise.resolve();token="new";return token},clear:()=>{}};
    const fetcher:typeof fetch=async (_url,init)=>{const h=new Headers(init?.headers);if((_url as string).endsWith('/auth/csrf')) return response(200,{csrf_token:'c'});calls++;return h.get('Authorization')==='Bearer new'?response(200,{ok:true}):response(401,{code:'EXPIRED'})};
    const client=new ApiClient("https://example.test",auth,fetcher); const [a,b]=await Promise.all([client.request<{ok:boolean}>("/api/v1/me"),client.request<{ok:boolean}>("/api/v1/me")]);
    expect(a.ok&&b.ok).toBe(true);expect(refreshes).toBe(1);expect(calls).toBe(4);
  });
  it("retries keyed POST after unknown network outcome and reuses key",async()=>{let calls=0;const seen:string[]=[];const auth:AuthStore={principal:()=>null,refresh:async()=>'',clear:()=>{}};const fetcher:typeof fetch=async(_u,i)=>{if((_u as string).endsWith('/auth/csrf'))return response(200,{csrf_token:'c'});calls++;seen.push(new Headers(i?.headers).get('Idempotency-Key')||'');if(calls===1)throw new TypeError('network');return response(200,{ok:true})};await new ApiClient('',auth,fetcher).request('/api/v1/tenants',{method:'POST',body:{name:'x'},idempotencyKey:'same'});expect(seen).toEqual(['same','same']);});
  it("uses native bootstrap header without browser CSRF",async()=>{let calls=0;const auth:AuthStore={principal:()=>null,refresh:async()=>'',clear:()=>{},clientHeaders:()=>({'X-Photo-Client':'native'})};const fetcher:typeof fetch=async(_u,i)=>{calls++;expect(new Headers(i?.headers).get('X-Photo-Client')).toBe('native');expect(new Headers(i?.headers).has('X-CSRF-Token')).toBe(false);return response(202,{challenge_id:'c'})};await new ApiClient('',auth,fetcher).request('/api/v1/auth/otp/request',{method:'POST',body:{email:'a@b.c',locale:'en'}});expect(calls).toBe(1)});
  it("sends mutation bodies as JCS",async()=>{let sent="";const auth:AuthStore={principal:()=>null,refresh:async()=>'',clear:()=>{},csrfToken:()=>"c"};const fetcher:typeof fetch=async(_u,i)=>{sent=String(i?.body);return response(200,{ok:true})};await new ApiClient('',auth,fetcher).request('/api/v1/tenants',{method:'POST',body:{z:-0,a:[1e30,.002]}});expect(sent).toBe('{"a":[1e+30,0.002],"z":0}')});
});
describe("state machines",()=>{it("keeps monotonic preview revisions across undo",()=>{const e=new EditorStore('v','a'.repeat(64),{x:1});e.apply({x:2});const old=e.state.draftRevision;e.undo();expect(e.state.draftRevision).toBeGreaterThan(old);expect(e.accepts({draftRevision:old,sourceSha:'a'.repeat(64)})).toBe(false)});it("clears frozen selection on scope change",()=>{const s=new SelectionStore();s.snapshot('x',12,'4');s.changeScope();expect(s.value).toBeNull()})});

describe('authenticated SSE transport',()=>{
 it('refreshes once and preserves resume and native headers',async()=>{
  let token='old',refreshes=0;
  const auth:AuthStore={principal:()=>({kind:'owner',accessToken:token}),refresh:async()=>{refreshes++;token='new';return token},clear:()=>{},clientHeaders:()=>({'X-Photo-Client':'native'})};
  const fetcher:typeof fetch=async(_url,init)=>{
   const headers=new Headers(init?.headers);
   expect(headers.get('Accept')).toBe('text/event-stream');
   expect(headers.get('Last-Event-ID')).toBe('10');
   expect(headers.get('X-Photo-Client')).toBe('native');
   if(headers.get('Authorization')==='Bearer old')return response(401);
   return new Response('id: 11\ndata: {"cursor":"11","type":"capture.alert","payload":{}}\n\n',{headers:{'Content-Type':'text/event-stream'}});
  };
  const events:unknown[]=[];
  await new ApiClient('https://example.test',auth,fetcher).eventStream('/api/v1/tenants/t/events','10',event=>events.push(event),new AbortController().signal);
  expect(refreshes).toBe(1);expect(events).toHaveLength(1);
 });
});
