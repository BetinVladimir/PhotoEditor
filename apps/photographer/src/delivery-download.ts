import {deliveryTimestamp,deliveryPackageEnvelope,readDeliveryPackage,type DeliveryPackage} from './delivery-package';
import type {DeliveryItem} from './delivery-items';

export type DeliveryDownloadTarget={kind:'manifest'}|{kind:'slot';ordinal:number;preset_id:string};
const uuid=(value:unknown):value is string=>typeof value==='string'&&/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value)&&value!=='00000000-0000-0000-0000-000000000000';
const record=(value:unknown):value is Record<string,unknown>=>!!value&&typeof value==='object'&&!Array.isArray(value);
function invalid():never{throw new Error('DELIVERY_DOWNLOAD_INVALID')}

export function deliveryDownloadTarget(pkg:DeliveryPackage,item?:DeliveryItem):DeliveryDownloadTarget{
 // Validate the same strict public DTO used by package reads before issuing a token.
 pkg=readDeliveryPackage(deliveryPackageEnvelope(pkg),pkg.tenant_id,pkg.project_id,pkg.id);
 if(pkg.state!=='ready'||pkg.published_at===null||pkg.document===null||pkg.document.artifact_id!==pkg.manifest_artifact_id)invalid();
 if(!item)return {kind:'manifest'};
 if(item.state!=='ready'||!uuid(item.id)||!uuid(item.export_id)||!uuid(item.preset_id)||
    typeof item.preset_sha!=='string'||!/^[0-9a-f]{64}$/.test(item.preset_sha)||
    typeof item.required!=='boolean'||item.reason!==null||!Number.isInteger(item.ordinal)||item.ordinal<0||item.ordinal>4999)invalid();
 return {kind:'slot',ordinal:item.ordinal,preset_id:item.preset_id};
}

export function readDeliveryDownloadIntent(value:unknown,now=Date.now(),maximumLifetimeMs=60000):string{
 if(!record(value)||Object.keys(value).length!==2||typeof value.intent_token!=='string'||!/^[A-Za-z0-9_-]{42}[AEIMQUYcgkosw048]$/.test(value.intent_token)||!deliveryTimestamp(value.expires_at))invalid();
 const expiry=Date.parse(value.expires_at as string);
 if(!Number.isFinite(expiry)||expiry<=now||expiry>now+maximumLifetimeMs||!Number.isSafeInteger(maximumLifetimeMs)||maximumLifetimeMs<1||maximumLifetimeMs>65000)invalid();
 return value.intent_token as string;
}

export function readDeliveryDownloadLink(value:unknown,expectedSHA?:string,now=Date.now(),maximumBytes=536870912,minimumValidityMs=5000):string{
 if(!record(value)||Object.keys(value).length!==5||typeof value.url!=='string'||!deliveryTimestamp(value.expires_at)||typeof value.filename!=='string'||typeof value.bytes!=='number'||typeof value.sha256!=='string')invalid();
 if(!Number.isSafeInteger(value.bytes)||value.bytes<=0||value.bytes>maximumBytes||!/^([0-9a-f]{64})$/.test(value.sha256 as string)||expectedSHA!==undefined&&value.sha256!==expectedSHA||!value.filename||value.filename.length>255||/[\x00-\x1f\/\\]/.test(value.filename as string)||value.url.length>16384)invalid();
 const expiry=Date.parse(value.expires_at as string);
 if(!Number.isFinite(expiry)||expiry<=now+minimumValidityMs||!Number.isSafeInteger(minimumValidityMs)||minimumValidityMs<0||minimumValidityMs>5000||expiry>now+65_000)invalid();
 let parsed:URL;
 try{parsed=new URL(value.url as string)}catch{return invalid()}
 const versions=parsed.searchParams.getAll('versionId');
 if(!['https:','http:'].includes(parsed.protocol)||!parsed.hostname||parsed.username||parsed.password||parsed.hash||versions.length!==1||!versions[0]||versions[0]==='null'||
    parsed.protocol==='http:'&&!['localhost','[::1]'].includes(parsed.hostname)&&!/^127(?:\.[0-9]{1,3}){3}$/.test(parsed.hostname))invalid();
 return parsed.toString();
}
