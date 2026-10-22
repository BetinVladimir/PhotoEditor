import { ApiClient, type AuthStore, type Principal } from "@photoeditor/api-client";
import * as SecureStore from "expo-secure-store";
let nativeStorageGeneration=0,nativeStorageWork:Promise<unknown>=Promise.resolve();
function serializeNative<T>(action:()=>Promise<T>){const work=nativeStorageWork.catch(()=>{}).then(action);nativeStorageWork=work;return work;}
async function settleNative(operations:Promise<unknown>[]){const settled=await Promise.allSettled(operations);const failed=settled.find(result=>result.status==='rejected');if(failed?.status==='rejected')throw failed.reason;}
const REFRESH_KEY="photoeditor.refresh.v1",CSRF_KEY="photoeditor.csrf.v1";
export class OwnerAuth implements AuthStore {
  private current: Principal|null=null;
  private acceptingEpoch:number|null=null;
  private epoch=0;
  private cachedClient?:{epoch:number;client:ApiClient};
  private listeners=new Set<()=>void>();
  sessionGeneration=()=>this.epoch;
  subscribeSession=(listener:()=>void)=>{this.listeners.add(listener);return()=>{this.listeners.delete(listener)}};
  private advanceSession(){this.epoch++;for(const listener of this.listeners)listener()}

  private csrf?:string;private refreshToken?:string;
  constructor(readonly origin:string,readonly clientType:"web"|"native",private transport:typeof fetch=fetch,private clearLocalData:()=>Promise<void>=async()=>{}){const send=transport;this.transport=(input,init)=>send(input,init);}
  principal(){return this.acceptingEpoch===this.epoch?null:this.current}
  csrfToken(){return this.csrf}
  clientHeaders():HeadersInit{return this.clientType==="native"?{"X-Photo-Client":"native"}:{}}
  setTokens(value:{access_token:string;refresh_token?:string;csrf_token:string},preserveSession=false){if(!value.access_token||!value.csrf_token)throw new Error("invalid tokens");this.current={kind:"owner",accessToken:value.access_token};this.csrf=value.csrf_token;if(value.refresh_token)this.refreshToken=value.refresh_token;if(!preserveSession)this.advanceSession()}
  async acceptLoginTokens(value:{access_token:string;refresh_token?:string;csrf_token:string},signal?:AbortSignal){
    signal?.throwIfAborted();
    if(!value.access_token||!value.csrf_token)throw new Error('invalid tokens');
    if(this.clientType==='native'&&!value.refresh_token)throw new Error('native token envelope incomplete');
    // Do not expose a usable principal until both durable native writes settle.
    this.acceptingEpoch=this.epoch+1;this.setTokens(value);const epoch=this.epoch;
    let rejectAbort!:(error:unknown)=>void;
    const aborted=new Promise<never>((_resolve,reject)=>{rejectAbort=reject;});
    const invalidate=()=>{if(this.epoch===epoch){void this.clear().catch(()=>{});}rejectAbort(new DOMException('Login cancelled','AbortError'));};
    signal?.addEventListener('abort',invalidate,{once:true});
    try{
      if(signal?.aborted)invalidate();
      await Promise.race([this.persist(),aborted]);signal?.throwIfAborted();
      if(this.epoch!==epoch)throw new DOMException('Auth session changed','AbortError');
      this.acceptingEpoch=null;this.advanceSession();return this.epoch;
    }catch(error){if(this.epoch===epoch)void this.clear().catch(()=>{});throw error;}
    finally{signal?.removeEventListener('abort',invalidate);}
  }
  async persist(){
    if(this.clientType!=='native')return;
    const epoch=this.epoch,refresh=this.refreshToken,csrf=this.csrf,intent=++nativeStorageGeneration;
    if(!refresh||!csrf)throw new Error('native token envelope incomplete');
    return serializeNative(async()=>{
      const current=()=>this.epoch===epoch&&nativeStorageGeneration===intent&&this.refreshToken===refresh&&this.csrf===csrf;
      if(!current())throw new DOMException('Auth session changed','AbortError');
      try{
        await settleNative([SecureStore.setItemAsync(REFRESH_KEY,refresh),SecureStore.setItemAsync(CSRF_KEY,csrf)]);
        if(!current())throw new DOMException('Auth session changed','AbortError');
      }catch(error){
        // The queue keeps any later accepted write behind cleanup, so logout
        // and a newer login cannot be overwritten by this late envelope.
        await settleNative([SecureStore.deleteItemAsync(REFRESH_KEY),SecureStore.deleteItemAsync(CSRF_KEY)]);
        throw error;
      }
    });
  }
  async restore(signal?:AbortSignal){
    signal?.throwIfAborted();const epoch=this.epoch;
    if(this.clientType==='native'){
      const intent=nativeStorageGeneration;
      const [refresh,csrf]=await serializeNative(()=>Promise.all([SecureStore.getItemAsync(REFRESH_KEY),SecureStore.getItemAsync(CSRF_KEY)]));
      signal?.throwIfAborted();if(this.epoch!==epoch||nativeStorageGeneration!==intent)return false;
      if(!refresh||!csrf)return false;
      this.refreshToken=refresh;this.csrf=csrf;
    }else if(!this.csrf){
      const bootstrap=await this.transport(`${this.origin.replace(/\/$/,"")}/api/v1/auth/csrf`,{credentials:'include',headers:{Accept:'application/json'},signal});
      signal?.throwIfAborted();if(!bootstrap.ok||this.epoch!==epoch)return false;
      const value=await bootstrap.json() as {csrf_token:string};signal?.throwIfAborted();if(this.epoch!==epoch)return false;this.csrf=value.csrf_token;
    }
    if(this.epoch!==epoch)return false;
    try{await this.refresh(signal);signal?.throwIfAborted();return this.epoch===epoch;}catch(error){if(signal?.aborted)throw error;if(this.epoch===epoch)await this.clear();return false;}
  }
  async refresh(signal?:AbortSignal){signal?.throwIfAborted();const epoch=this.epoch;const headers=new Headers({Accept:"application/json","Content-Type":"application/json"});if(this.csrf)headers.set("X-CSRF-Token",this.csrf);if(this.clientType==="native")headers.set("X-Photo-Client","native");const response=await this.transport(`${this.origin.replace(/\/$/,"")}/api/v1/auth/refresh`,{method:"POST",credentials:"include",headers,body:JSON.stringify({refresh_token:this.clientType==="native"?this.refreshToken||"":"",client_type:this.clientType}),signal});signal?.throwIfAborted();if(!response.ok)throw new Error("refresh failed");const body=await response.json() as {access_token:string;refresh_token?:string;csrf_token:string};signal?.throwIfAborted();if(epoch!==this.epoch)throw new Error("auth session changed");this.setTokens(body,true);await this.persist();return body.access_token}
  async clear(){
    const intent=this.clientType==='native'?++nativeStorageGeneration:0;
    this.acceptingEpoch=null;this.current=null;this.csrf=undefined;this.refreshToken=undefined;this.advanceSession();
    const cleanup=this.clientType==='native'?serializeNative(async()=>{if(nativeStorageGeneration===intent)await settleNative([SecureStore.deleteItemAsync(REFRESH_KEY),SecureStore.deleteItemAsync(CSRF_KEY)]);}):Promise.resolve();
    await Promise.all([this.clearLocalData(),cleanup]);
  }
  client(){
    const epoch=this.epoch;if(this.cachedClient?.epoch===epoch)return this.cachedClient.client;
    const active=()=>{if(this.epoch!==epoch)throw new DOMException("Auth session changed","AbortError")};
    const store:AuthStore={principal:()=>{active();return this.principal()},csrfToken:()=>{active();return this.csrfToken()},clientHeaders:()=>{active();return this.clientHeaders()},refresh:async signal=>{active();const token=await this.refresh(signal);active();return token},clear:()=>{if(this.epoch===epoch)return this.clear()}};
    const transport:typeof fetch=async(input,init)=>{active();const response=await this.transport(input,init);if(this.epoch!==epoch){await response.body?.cancel().catch(()=>{});active()}return response};
    const client=new ApiClient(this.origin,store,transport);this.cachedClient={epoch,client};return client;
  }
}
