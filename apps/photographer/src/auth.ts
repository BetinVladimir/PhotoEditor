import { ApiClient, type AuthStore, type Principal } from "@photoeditor/api-client";
import * as SecureStore from "expo-secure-store";
const REFRESH_KEY="photoeditor.refresh.v1",CSRF_KEY="photoeditor.csrf.v1";
export class OwnerAuth implements AuthStore {
  private current: Principal|null=null;
  private csrf?:string;private refreshToken?:string;
  constructor(readonly origin:string,readonly clientType:"web"|"native",private transport:typeof fetch=fetch,private clearLocalData:()=>Promise<void>=async()=>{}){}
  principal(){return this.current}
  csrfToken(){return this.csrf}
  clientHeaders():HeadersInit{return this.clientType==="native"?{"X-Photo-Client":"native"}:{}}
  setTokens(value:{access_token:string;refresh_token?:string;csrf_token:string}){if(!value.access_token||!value.csrf_token)throw new Error("invalid tokens");this.current={kind:"owner",accessToken:value.access_token};this.csrf=value.csrf_token;if(value.refresh_token)this.refreshToken=value.refresh_token}
  async persist(){if(this.clientType!=="native")return;if(!this.refreshToken||!this.csrf)throw new Error("native token envelope incomplete");await Promise.all([SecureStore.setItemAsync(REFRESH_KEY,this.refreshToken),SecureStore.setItemAsync(CSRF_KEY,this.csrf)])}
  async restore(){if(this.clientType==="native"){[this.refreshToken,this.csrf]=await Promise.all([SecureStore.getItemAsync(REFRESH_KEY),SecureStore.getItemAsync(CSRF_KEY)]).then(x=>[x[0]||undefined,x[1]||undefined]);if(!this.refreshToken||!this.csrf)return false}else if(!this.csrf){const bootstrap=await this.transport(`${this.origin.replace(/\/$/,"")}/api/v1/auth/csrf`,{credentials:"include",headers:{Accept:"application/json"}});if(!bootstrap.ok)return false;this.csrf=((await bootstrap.json())as{csrf_token:string}).csrf_token}try{await this.refresh();return true}catch{await this.clear();return false}}
  async refresh(signal?:AbortSignal){const headers=new Headers({Accept:"application/json","Content-Type":"application/json"});if(this.csrf)headers.set("X-CSRF-Token",this.csrf);if(this.clientType==="native")headers.set("X-Photo-Client","native");const response=await this.transport(`${this.origin.replace(/\/$/,"")}/api/v1/auth/refresh`,{method:"POST",credentials:"include",headers,body:JSON.stringify({refresh_token:this.clientType==="native"?this.refreshToken||"":"",client_type:this.clientType}),signal});if(!response.ok)throw new Error("refresh failed");const body=await response.json() as {access_token:string;refresh_token?:string;csrf_token:string};this.setTokens(body);await this.persist();return body.access_token}
  async clear(){this.current=null;this.csrf=undefined;this.refreshToken=undefined;await Promise.all([this.clearLocalData(),...(this.clientType==="native"?[SecureStore.deleteItemAsync(REFRESH_KEY),SecureStore.deleteItemAsync(CSRF_KEY)]:[])])}
  client(){return new ApiClient(this.origin,this,this.transport)}
}
