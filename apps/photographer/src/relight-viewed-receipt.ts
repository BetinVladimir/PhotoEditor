export type RelightViewedReceipt={version:1;token:string;expires_at:string};
export function pendingReviewImageToken(raw:string,origin:string):string{
 const url=new URL(raw),base=new URL(origin);
 if(url.origin!==base.origin||url.pathname!=='/api/v1/media/review-pending'||url.hash||url.username||url.password||[...url.searchParams.keys()].sort().join(',')!=='token,versionId')throw new Error('RELIGHT_VIEW_PAIR_INVALID');
 const token=url.searchParams.get('token'),version=url.searchParams.get('versionId');
 if(!token||token.length>1600||!version||!/^[-_A-Za-z0-9]+\.[-_A-Za-z0-9]+$/.test(token))throw new Error('RELIGHT_VIEW_PAIR_INVALID');
 return token;
}
export function readRelightViewedReceipt(value:unknown,now=Date.now()):RelightViewedReceipt{
 const v=value as RelightViewedReceipt;
 if(!v||typeof v!=='object'||Array.isArray(v)||Object.keys(v).sort().join(',')!=='expires_at,token,version'||v.version!==1||typeof v.token!=='string'||v.token.length>4200||!/^[-_A-Za-z0-9]+\.[-_A-Za-z0-9]+$/.test(v.token)||typeof v.expires_at!=='string'||!/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})$/.test(v.expires_at))throw new Error('RELIGHT_VIEW_RECEIPT_INVALID');
 const expiry=Date.parse(v.expires_at);
 if(!Number.isFinite(expiry)||expiry<=now||expiry>now+120000)throw new Error('RELIGHT_VIEW_RECEIPT_INVALID');
 return {...v};
}
