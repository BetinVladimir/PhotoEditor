type ClientType='web'|'native';
type Attempt<T>={key:string;body:T};
/** Ephemeral login recovery only: never persist OTP codes or bootstrap secrets. */
export class LoginAttempts{
 private request?:Attempt<{email:string;locale:string}>;
 private verify?:Attempt<{challenge_id:string;code:string;client_type:ClientType}>;
 private bootstrap?:string;
 private readonly nonce:string;
 constructor(readonly clientType:ClientType){
  const bytes=crypto.getRandomValues(new Uint8Array(32)),alphabet='ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';let result='';
  for(let offset=0;offset<bytes.length;offset+=3){const remaining=bytes.length-offset,n=(bytes[offset]<<16)|((bytes[offset+1]??0)<<8)|(bytes[offset+2]??0);result+=alphabet[(n>>>18)&63]+alphabet[(n>>>12)&63];if(remaining>1)result+=alphabet[(n>>>6)&63];if(remaining>2)result+=alphabet[n&63];}this.nonce=result;
 }
 requestAttempt(email:string,locale:string){if(!this.request||this.request.body.email!==email||this.request.body.locale!==locale)this.request={key:crypto.randomUUID(),body:{email,locale}};return structuredClone(this.request);}
 verifyAttempt(challenge:string,code:string){if(!this.verify||this.verify.body.challenge_id!==challenge||this.verify.body.code!==code)this.verify={key:crypto.randomUUID(),body:{challenge_id:challenge,code,client_type:this.clientType}};return structuredClone(this.verify);}
 async headers(read:(signal:AbortSignal)=>Promise<{csrf_token:string}>,signal:AbortSignal):Promise<Record<string,string>>{
  signal.throwIfAborted();if(this.clientType==='native')return{'X-Photo-Auth-Nonce':this.nonce};
  if(!this.bootstrap){const value=await read(signal);signal.throwIfAborted();if(typeof value.csrf_token!=='string'||!value.csrf_token||value.csrf_token.length>4096)throw new Error('LOGIN_BOOTSTRAP_INVALID');this.bootstrap=value.csrf_token;}
  return{'X-CSRF-Token':this.bootstrap};
 }
 newChallenge(){this.request=undefined;this.verify=undefined;this.bootstrap=undefined;}
}
