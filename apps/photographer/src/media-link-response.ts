import type {MediaLink} from './catalog-types';
const edges={thumb320:320,thumb640:640,preview1600:1600,preview2560:2560};
const digest=(v:unknown)=>typeof v==='string'&&/^[0-9a-f]{64}$/.test(v);
export function readMediaLinks(raw:unknown,version:string,variants:MediaLink['variant'][],now=Date.now()):{items:MediaLink[]}{
 const fail=():never=>{throw Error('MEDIA_LINK_RESPONSE_INVALID')};
 if(!raw||typeof raw!=='object'||Array.isArray(raw)||Object.keys(raw).join(',')!=='items')return fail();
 const rows=(raw as {items:unknown}).items;if(!Array.isArray(rows)||rows.length!==variants.length)return fail();
 const seen=new Set<string>(),items:MediaLink[]=[];
 for(const rawRow of rows){
  if(!rawRow||typeof rawRow!=='object'||Array.isArray(rawRow))return fail();const v=rawRow as Record<string,unknown>;
  if(Object.keys(v).sort().join(',')!==['asset_version_id','artifact_sha256','icc_sha256','profile_hash','variant','url','expires_at','width','height','media_type'].sort().join(',')||v.asset_version_id!==version||typeof v.variant!=='string'||!variants.includes(v.variant as MediaLink['variant'])||seen.has(v.variant)||!digest(v.artifact_sha256)||!digest(v.icc_sha256)||!digest(v.profile_hash)||typeof v.url!=='string'||v.url.length>8192||typeof v.expires_at!=='string'||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+605000||!Number.isSafeInteger(v.width)||!Number.isSafeInteger(v.height)||Number(v.width)<1||Number(v.height)<1||Math.max(Number(v.width),Number(v.height))>edges[v.variant as keyof typeof edges]||!['image/jpeg','image/webp'].includes(v.media_type as string))return fail();
  let url:URL;try{url=new URL(v.url)}catch{return fail()}
  const versions=url.searchParams.getAll('versionId');
  if(!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||!(url.protocol==='https:'||url.protocol==='http:'&&(['localhost','[::1]'].includes(url.hostname)||/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))))return fail();
  seen.add(v.variant);items.push({...v} as MediaLink);
 }
 return {items};
}
