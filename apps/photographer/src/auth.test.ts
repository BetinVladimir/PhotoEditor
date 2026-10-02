import{describe,expect,it}from"vitest";import{OwnerAuth}from"./auth";
const json=(body:unknown,status=200)=>new Response(JSON.stringify(body),{status,headers:{"Content-Type":"application/json"}});
describe("OwnerAuth web restore",()=>it("bootstraps CSRF then rotates HttpOnly-cookie refresh",async()=>{const calls:Array<{url:string;init?:RequestInit}>=[];const transport:typeof fetch=async(input,init)=>{const url=String(input);calls.push({url,init});if(url.endsWith('/auth/csrf'))return json({csrf_token:'bootstrap'});return json({access_token:'access',csrf_token:'session'})};const auth=new OwnerAuth("https://app.test","web",transport);expect(await auth.restore()).toBe(true);expect(auth.principal()).toEqual({kind:"owner",accessToken:"access"});expect(calls).toHaveLength(2);const headers=new Headers(calls[1].init?.headers);expect(headers.get('X-CSRF-Token')).toBe('bootstrap');expect(JSON.parse(String(calls[1].init?.body))).toEqual({refresh_token:'',client_type:'web'});expect(auth.csrfToken()).toBe('session')}));

it("clears local drafts when owner credentials are cleared",async()=>{
 let clears=0;
 const auth=new OwnerAuth("https://app.test","web",fetch,async()=>{clears++});
 auth.setTokens({access_token:"access",csrf_token:"csrf"});
 await auth.clear();expect(auth.principal()).toBeNull();expect(clears).toBe(1);
});
