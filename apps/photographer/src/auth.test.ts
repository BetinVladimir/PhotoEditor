import{describe,expect,it}from"vitest";import{OwnerAuth}from"./auth";
it('does not bind browser fetch to OwnerAuth during bootstrap, refresh or delegated API calls',async()=>{
 const receivers:unknown[]=[];
 const transport:typeof fetch=async function(this:unknown,input){receivers.push(this);if(this!==undefined)throw new TypeError('Illegal invocation');return json(String(input).endsWith('/csrf')?{csrf_token:'bootstrap'}:String(input).endsWith('/refresh')?{access_token:'access',csrf_token:'session'}:{id:'actor'});};
 const auth=new OwnerAuth('http://localhost:58080','web',transport);expect(await auth.restore()).toBe(true);await expect(auth.client().request('/api/v1/me')).resolves.toEqual({id:'actor'});expect(receivers).toEqual([undefined,undefined,undefined]);
});
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});
describe("OwnerAuth web restore",()=>it("bootstraps CSRF then rotates HttpOnly-cookie refresh",async()=>{const calls:Array<{url:string;init?:RequestInit}>=[];const transport:typeof fetch=async(input,init)=>{const url=String(input);calls.push({url,init});if(url.endsWith('/auth/csrf'))return json({csrf_token:'bootstrap'});return json({access_token:'access',csrf_token:'session'})};const auth=new OwnerAuth("https://app.test","web",transport);expect(await auth.restore()).toBe(true);expect(auth.principal()).toEqual({kind:"owner",accessToken:"access"});expect(calls).toHaveLength(2);const headers=new Headers(calls[1].init?.headers);expect(headers.get('X-CSRF-Token')).toBe('bootstrap');expect(JSON.parse(String(calls[1].init?.body))).toEqual({refresh_token:'',client_type:'web'});expect(auth.csrfToken()).toBe('session')}));

it("clears local drafts when owner credentials are cleared",async()=>{
 let clears=0;
 const auth=new OwnerAuth("https://app.test","web",fetch,async()=>{clears++});
 auth.setTokens({access_token:"access",csrf_token:"csrf"});
 await auth.clear();expect(auth.principal()).toBeNull();expect(clears).toBe(1);
});

it('keeps clients stable within a session and fences clients across replacement',async()=>{
 let calls=0;const auth=new OwnerAuth('https://app.test','web',async()=>{calls++;return json({})});
 auth.setTokens({access_token:'first',csrf_token:'one'});const old=auth.client();expect(auth.client()).toBe(old);
 auth.setTokens({access_token:'second',csrf_token:'two'});expect(auth.client()).not.toBe(old);
 await expect(old.request('/api/v1/me')).rejects.toThrow('Auth session changed');expect(calls).toBe(0);
});
it('rejects a late refresh after account replacement without installing its token',async()=>{
 let finish!:(response:Response)=>void;const auth=new OwnerAuth('https://app.test','web',()=>new Promise(resolve=>{finish=resolve}));
 auth.setTokens({access_token:'first',csrf_token:'one'});const pending=auth.refresh();
 auth.setTokens({access_token:'second',csrf_token:'two'});finish(json({access_token:'late',csrf_token:'late'}));
 await expect(pending).rejects.toThrow('auth session changed');expect(auth.principal()).toEqual({kind:'owner',accessToken:'second'});
});
it('notifies source scopes on login and logout but preserves generation for token refresh',async()=>{
 const auth=new OwnerAuth('https://app.test','web',async()=>json({access_token:'refreshed',csrf_token:'new'}));let changes=0;const stop=auth.subscribeSession(()=>{changes++});
 auth.setTokens({access_token:'first',csrf_token:'one'});const epoch=auth.sessionGeneration(),client=auth.client();await auth.refresh();expect(auth.sessionGeneration()).toBe(epoch);expect(auth.client()).toBe(client);
 await auth.clear();expect(changes).toBe(2);stop();
});
it('accepts and persists login in one epoch-bound call instead of using an invalidated caller proxy',async()=>{
 const auth=new OwnerAuth('https://app.test','web');const before=auth.sessionGeneration();
 await auth.acceptLoginTokens({access_token:'login',csrf_token:'session'});expect(auth.sessionGeneration()).toBe(before+2);expect(auth.principal()).toEqual({kind:'owner',accessToken:'login'});
});
