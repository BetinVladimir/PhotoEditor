import type {MultiProposal} from './multi-proposal';
export type MultiPreview={schema_version:1;tenant_id:string;project_id:string;operation:MultiProposal['operation'];producer_task_id:string;result_id:string;variant:'preview1600'|'preview2560';artifact:{artifact_id:string;sha256:string};bytes:number;media_type:'image/jpeg'|'image/webp';width:number;height:number;url:string;expires_at:string};
export function readMultiPreview(value:unknown,proposal:MultiProposal,variant:MultiPreview['variant'],now=Date.now()):MultiPreview{
 const fail=()=>{throw new Error('MULTI_PREVIEW_INVALID');};

 const keys=['schema_version','tenant_id','project_id','operation','producer_task_id','result_id','variant','artifact','bytes','media_type','width','height','url','expires_at'];
 if(!value||typeof value!=='object'||Array.isArray(value)||Object.keys(value).length!==keys.length||!keys.every(k=>Object.hasOwn(value,k)))fail();
 const v=value as MultiPreview;
 if(v.schema_version!==1||v.tenant_id!==proposal.tenant_id||v.project_id!==proposal.project_id||v.operation!==proposal.operation||v.producer_task_id!==proposal.producer_task_id||v.result_id!==proposal.result_id||v.variant!==variant||!['preview1600','preview2560'].includes(variant))fail();
 if(!v.artifact||Object.keys(v.artifact).length!==2||!Object.hasOwn(v.artifact,'artifact_id')||!Object.hasOwn(v.artifact,'sha256')||typeof v.artifact.artifact_id!=='string'||!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(v.artifact.artifact_id)||v.artifact.artifact_id==='00000000-0000-0000-0000-000000000000'||typeof v.artifact.sha256!=='string'||!/^[0-9a-f]{64}$/.test(v.artifact.sha256))fail();
 if([proposal.result,proposal.working_image,proposal.changed_mask,proposal.report,...proposal.registrations,...proposal.sources.map(s=>s.source)].some(ref=>ref.artifact_id===v.artifact.artifact_id))fail();
 const edge=variant==='preview1600'?1600:2560;
 if(!Number.isSafeInteger(v.bytes)||v.bytes<2||v.bytes>54525952||!Number.isSafeInteger(v.width)||!Number.isSafeInteger(v.height)||v.width<1||v.height<1||v.width>edge||v.height>edge||!['image/jpeg','image/webp'].includes(v.media_type))fail();
 const scale=Math.min(1,edge/Math.max(proposal.canvas.width,proposal.canvas.height));
 const roundEven=(n:number)=>{const f=Math.floor(n),d=n-f;return d===0.5?(f%2===0?f:f+1):Math.round(n);};
 if(v.width!==Math.max(1,roundEven(proposal.canvas.width*scale))||v.height!==Math.max(1,roundEven(proposal.canvas.height*scale))||v.media_type!=='image/webp')fail();
 if(typeof v.expires_at!=='string'||!Number.isFinite(Date.parse(v.expires_at))||Date.parse(v.expires_at)<=now||Date.parse(v.expires_at)>now+120000)fail();
 if(typeof v.url!=='string')fail();
 const url=new URL(v.url);
 const versions=url.searchParams.getAll('versionId');
 if(!['https:','http:'].includes(url.protocol)||!url.hostname||url.username||url.password||url.hash||versions.length!==1||!versions[0]||versions[0]==='null'||
    url.protocol==='http:'&&!['localhost','[::1]'].includes(url.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(url.hostname))fail();
 return structuredClone(v);
}
