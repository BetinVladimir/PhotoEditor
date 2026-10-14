import type {DeliveryCreateAttemptStore} from './delivery-create-attempt-store';
import {useEffect,useMemo,useRef,useState} from 'react';
import {ActivityIndicator,FlatList,Pressable,SafeAreaView,Text,TextInput,View} from 'react-native';
import {APIError,type ApiClient} from '@photoeditor/api-client';
import {OwnerAPI} from './owner-api';
import type {ExportPreset} from './catalog-types';
import {freezeDeliveryCreate,type DeliveryCreateRequest,type DeliveryCreateTicket,type DeliveryMetadataField} from './delivery-create';
import {catalogStyle as s} from './catalog-style';
import {useLocale} from './locale-context';
import {assertDeliveryScope} from './delivery-package';
import type {DeliverySigningPolicyChoice} from './delivery-signing-policies';
type Props={journal?:DeliveryCreateAttemptStore;client:ApiClient;tenant:string;project:string;snapshot:string;onCreated:(ticket:DeliveryCreateTicket)=>void};
export function DeliveryCreateScreen(props:Props){const generation=useMemo(()=>crypto.randomUUID(),[props.client,props.journal]);return <Scope key={`${generation}:${props.tenant}:${props.project}:${props.snapshot}`} {...props}/>;}
function Scope({client,tenant,project,snapshot,onCreated,journal}:Props){
 const {t}=useLocale(),api=useMemo(()=>new OwnerAPI(client,tenant),[client,tenant]);
 const [presets,setPresets]=useState<ExportPreset[]>([]),[cursor,setCursor]=useState<string|null>(null),[name,setName]=useState(''),[required,setRequired]=useState<string[]>([]),[optional,setOptional]=useState<string[]>([]);
 const [metadataPolicy,setMetadataPolicy]=useState<DeliveryCreateRequest['policy']['metadata_policy']>('strip'),[approvedFields,setApprovedFields]=useState<DeliveryMetadataField[]>([]);
 const [provenance,setProvenance]=useState<DeliveryCreateRequest['policy']['provenance']>('off'),[policies,setPolicies]=useState<DeliverySigningPolicyChoice[]>([]),[selectedPolicy,setSelectedPolicy]=useState<Pick<DeliverySigningPolicyChoice,'id'|'sha256'>|null>(null),[policyLoading,setPolicyLoading]=useState(false),[policyError,setPolicyError]=useState(false);
 const [recovered,setRecovered]=useState(!journal);
 const [review,setReview]=useState(true),[omitted,setOmitted]=useState(false),[busy,setBusy]=useState(false),[locked,setLocked]=useState(false),[denied,setDenied]=useState(false),[error,setError]=useState('');
 const alive=useRef(false),flight=useRef<AbortController|null>(null),attempt=useRef<{body:DeliveryCreateRequest;key:string}|null>(null),done=useRef(false),usedCursors=useRef(new Set<string>());
 const selectedFormats=useMemo(()=>[...required,...optional].map(id=>presets.find(preset=>preset.id===id)?.config.format||''),[required,optional,presets]);
 const selectedFormatKey=useMemo(()=>[...new Set(selectedFormats)].sort().join(','),[selectedFormats]);
 const hasRequired=required.length>0;
 useEffect(()=>{alive.current=true;return()=>{alive.current=false;flight.current?.abort();flight.current=null};},[]);
 function failure(reason:unknown){
  if(reason instanceof APIError&&[401,403,404,410].includes(reason.status)){attempt.current=null;setPresets([]);setRequired([]);setOptional([]);setDenied(true);setLocked(false);setMetadataPolicy('strip');setApprovedFields([]);setProvenance('off');setPolicies([]);setSelectedPolicy(null);}
  setError(t('delivery_create_failed'));
 }
 async function load(after?:string){
  if(flight.current||done.current)return;
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{assertDeliveryScope(tenant,project,snapshot);
   if(after&&usedCursors.current.has(after))throw new Error('DELIVERY_PRESET_CURSOR_REPEATED');
   if(!after)usedCursors.current.clear();
   if(!after&&journal){journal.assertScope(tenant,project,snapshot);const saved=await journal.read();if(!alive.current||controller.signal.aborted)return;
    if(saved){attempt.current={body:saved.body,key:saved.key};setLocked(true);setName(saved.body.name);setRequired(saved.body.policy.required_presets);setOptional(saved.body.policy.optional_presets);setReview(saved.body.policy.require_review);setOmitted(saved.body.policy.allow_omitted);setMetadataPolicy(saved.body.policy.metadata_policy);setApprovedFields(saved.body.policy.approved_fields||[]);setProvenance(saved.body.policy.provenance);setSelectedPolicy(saved.body.policy.provenance==='required'?{id:saved.body.policy.signing_policy_id!,sha256:saved.body.policy.signing_policy_sha!}:null);
     if(saved.ticket){done.current=true;onCreated(saved.ticket);return;}
    }
    setRecovered(true);
   }
   const page=await api.presets(after,controller.signal);if(alive.current&&!controller.signal.aborted){
    if(!page||!Array.isArray(page.items)||page.items.length>100||!(page.next_cursor===null||typeof page.next_cursor==='string'&&page.next_cursor.length>0&&page.next_cursor.length<=4096)||page.items.length===0&&page.next_cursor!==null)throw new Error('DELIVERY_PRESET_PAGE_INVALID');
    const ids=new Set(after?presets.map(p=>p.id):[]);
    for(const preset of page.items){if(!preset||typeof preset.id!=='string'||ids.has(preset.id))throw new Error('DELIVERY_PRESET_PAGE_INVALID');ids.add(preset.id);}
    if(page.next_cursor&&(page.next_cursor===after||usedCursors.current.has(page.next_cursor)))throw new Error('DELIVERY_PRESET_CURSOR_REPEATED');
    if(after)usedCursors.current.add(after);
    setPresets(prior=>after?[...prior,...page.items]:page.items);setCursor(page.next_cursor);
   }}
  catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller)flight.current=null;if(alive.current&&!controller.signal.aborted)setBusy(false);}
 }
 useEffect(()=>{void load();return()=>{flight.current?.abort();flight.current=null};},[api]);
 useEffect(()=>{
  if(locked||denied){setPolicyLoading(false);return;}
  setPolicies([]);setSelectedPolicy(null);setPolicyError(false);
  if(provenance!=='required'){setPolicyLoading(false);return;}
  if(!hasRequired||selectedFormatKey.split(',').includes('')){setPolicyLoading(false);return;}
  const controller=new AbortController();let live=true;setPolicyLoading(true);
  void api.deliverySigningPolicies(project,selectedFormatKey.split(',') as Array<'jpeg'|'png'|'tiff'>,controller.signal)
   .then(choices=>{if(live&&!controller.signal.aborted)setPolicies(choices)})
   .catch(cause=>{if(live&&!controller.signal.aborted){setPolicyError(true);if(cause instanceof APIError&&[401,403,404].includes(cause.status)){setDenied(true);setPresets([]);setRequired([]);setOptional([]);}}})
   .finally(()=>{if(live&&!controller.signal.aborted)setPolicyLoading(false)});
  return()=>{live=false;controller.abort()};
 },[api,project,provenance,selectedFormatKey,hasRequired,locked,denied]);
 function choose(id:string,kind:'required'|'optional'){
  if(!recovered||locked||busy||denied)return;
  const preset=presets.find(p=>p.id===id);if(!preset||(preset.config.metadata_policy||'strip')!==metadataPolicy)return;
  const current=kind==='required'?required:optional;
  if(!required.includes(id)&&!optional.includes(id)&&required.length+optional.length>=16)return;
  const nextRequired=kind==='required'?(current.includes(id)?current.filter(x=>x!==id):[...current,id]):required.filter(x=>x!==id);
  const nextOptional=kind==='optional'?(current.includes(id)?current.filter(x=>x!==id):[...current,id]):optional.filter(x=>x!==id);
  const nextFormatKey=[...new Set([...nextRequired,...nextOptional].map(value=>presets.find(p=>p.id===value)?.config.format||''))].sort().join(',');
  if(nextFormatKey!==selectedFormatKey)setSelectedPolicy(null);
  setRequired(nextRequired);setOptional(nextOptional);
 }
 function selectMetadata(mode:DeliveryCreateRequest['policy']['metadata_policy']){
  if(!recovered||locked||busy||denied||mode===metadataPolicy)return;
  setMetadataPolicy(mode);setApprovedFields([]);setRequired([]);setOptional([]);setSelectedPolicy(null);
 }
 function selectProvenance(mode:DeliveryCreateRequest['policy']['provenance']){
  if(!recovered||locked||busy||denied||mode===provenance)return;
  setProvenance(mode);setSelectedPolicy(null);
 }
 function approve(field:DeliveryMetadataField){
  if(!recovered||locked||busy||denied||metadataPolicy!=='approved_exif')return;
  setApprovedFields(prior=>prior.includes(field)?prior.filter(v=>v!==field):[...prior,field]);
 }
 const metadataModes=[['strip','delivery_metadata_strip'],['copyright','delivery_metadata_copyright'],['approved_exif','delivery_metadata_approved']] as const;
 const metadataFields=[['author','delivery_metadata_author'],['copyright','delivery_metadata_rights'],['camera_make','delivery_metadata_make'],['camera_model','delivery_metadata_model'],['lens','delivery_metadata_lens'],['datetime_original','delivery_metadata_datetime'],['gps','delivery_metadata_gps']] as const;
 async function submit(){
  if(!recovered||flight.current||denied||done.current)return;
  let owned=attempt.current;
  if(!owned){try{if(provenance==='required'&&(policyLoading||!selectedPolicy||!policies.some(choice=>choice.id===selectedPolicy.id&&choice.sha256===selectedPolicy.sha256)))throw new Error('DELIVERY_SIGNING_CHOICE_INVALID');const key=crypto.randomUUID();owned={key,body:freezeDeliveryCreate({project_id:project,snapshot_id:snapshot,name:name.trim(),policy:{required_presets:required,optional_presets:optional,require_review:review,allow_omitted:omitted,metadata_policy:metadataPolicy,approved_fields:metadataPolicy==='approved_exif'?approvedFields:[],provenance,...(provenance==='required'?{signing_policy_id:selectedPolicy!.id,signing_policy_sha:selectedPolicy!.sha256}:{})}},key)};attempt.current=owned;setLocked(true);}catch{setError(t('delivery_create_invalid'));return;}}
  const controller=new AbortController();flight.current=controller;setBusy(true);setError('');
  try{
   if(journal){
    journal.assertScope(tenant,project,snapshot);
    const saved=await journal.read();if(!alive.current||controller.signal.aborted)return;
    if(saved){
     if(saved.key!==owned.key||JSON.stringify(saved.body)!==JSON.stringify(owned.body))throw new Error('DELIVERY_CREATE_JOURNAL_CONFLICT');
     if(saved.ticket){done.current=true;onCreated(saved.ticket);return;}
    }
    await journal.write({version:1,key:owned.key,body:owned.body,ticket:null});
   }
   if(!alive.current||controller.signal.aborted)return;
   const ticket=await api.createDelivery(owned.body,owned.key,controller.signal);
   if(!alive.current||controller.signal.aborted)return;
   if(journal)await journal.write({version:1,key:owned.key,body:owned.body,ticket});
   if(alive.current&&!controller.signal.aborted){done.current=true;onCreated(ticket);}
  }
  catch(reason){if(alive.current&&!controller.signal.aborted)failure(reason);}
  finally{if(flight.current===controller)flight.current=null;if(alive.current&&!controller.signal.aborted)setBusy(false);}
 }
 return <SafeAreaView style={s.root}><View style={s.header}><Text style={s.title}>{t('delivery_create_title')}</Text>
 <TextInput accessibilityLabel={t('delivery_create_name')} value={name} onChangeText={setName} editable={recovered&&!busy&&!locked&&!denied} placeholder={t('delivery_create_name')} style={s.input}/>
 <Pressable accessibilityRole="checkbox" accessibilityState={{checked:review,disabled:!recovered||busy||locked||denied}} disabled={!recovered||busy||locked||denied} onPress={()=>setReview(!review)}><Text style={s.name}>{review?'☑':'☐'} {t('delivery_create_review')}</Text></Pressable>
 <Pressable accessibilityRole="checkbox" accessibilityState={{checked:omitted,disabled:!recovered||busy||locked||denied}} disabled={!recovered||busy||locked||denied} onPress={()=>setOmitted(!omitted)}><Text style={s.name}>{omitted?'☑':'☐'} {t('delivery_create_omitted')}</Text></Pressable>
 <Text style={s.subtitle}>{t('delivery_metadata_title')}</Text>
 {metadataModes.map(([mode,label])=><Pressable key={mode} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:metadataPolicy===mode,disabled:!recovered||busy||locked||denied}} disabled={!recovered||busy||locked||denied} onPress={()=>selectMetadata(mode)}><Text style={s.name}>{metadataPolicy===mode?'◉':'○'} {t(label)}</Text></Pressable>)}
 <Text style={s.meta}>{t('delivery_metadata_presets_hint')}</Text>
 {metadataPolicy==='approved_exif'?<View><Text style={s.meta}>{t('delivery_metadata_consent_hint')}</Text>{metadataFields.map(([field,label])=><Pressable key={field} accessibilityRole="checkbox" accessibilityLabel={t(label)} accessibilityState={{checked:approvedFields.includes(field),disabled:!recovered||busy||locked||denied}} disabled={!recovered||busy||locked||denied} onPress={()=>approve(field)}><Text style={s.name}>{approvedFields.includes(field)?'☑':'☐'} {t(label)}</Text></Pressable>)}</View>:null}
 <Text style={s.subtitle}>{t('delivery_provenance_title')}</Text>
 {([['off','delivery_provenance_off'],['required','delivery_provenance_required']] as const).map(([mode,label])=><Pressable key={mode} accessibilityRole="radio" accessibilityLabel={t(label)} accessibilityState={{checked:provenance===mode,disabled:!recovered||busy||locked||denied}} disabled={!recovered||busy||locked||denied} onPress={()=>selectProvenance(mode)}><Text style={s.name}>{provenance===mode?'◉':'○'} {t(label)}</Text></Pressable>)}
 {provenance==='required'&&locked&&selectedPolicy?<Text style={s.meta}>{t('delivery_signing_locked')} {selectedPolicy.id.slice(0,8)}</Text>:null}
 {provenance==='required'&&!locked?policyLoading?<ActivityIndicator/>:<View>{policies.map(choice=><Pressable key={choice.id} accessibilityRole="radio" accessibilityLabel={`${t('delivery_signing_policy')} ${choice.id.slice(0,8)}`} accessibilityState={{checked:selectedPolicy?.id===choice.id,disabled:busy||denied}} disabled={busy||denied} onPress={()=>setSelectedPolicy({id:choice.id,sha256:choice.sha256})}><Text style={s.name}>{selectedPolicy?.id===choice.id?'◉':'○'} {t('delivery_signing_policy')} {choice.id.slice(0,8)} · {choice.revision}</Text></Pressable>)}{(policyError||policies.length===0)?<Text style={s.meta}>{t('delivery_signing_unavailable')}</Text>:null}</View>:null}
 {error?<Text accessibilityRole="alert" style={s.error}>{error}</Text>:null}{busy?<ActivityIndicator/>:null}
 {error&&!busy&&!locked&&!denied?<Pressable accessibilityRole="button" onPress={()=>void load()} style={s.button}><Text style={s.buttonText}>{t('delivery_create_reload')}</Text></Pressable>:null}
 <Pressable accessibilityRole="button" disabled={!recovered||busy||denied||done.current||(!locked&&(!name.trim()||!required.length||provenance==='required'&&(policyLoading||!selectedPolicy)))} onPress={()=>void submit()} style={s.button}><Text style={s.buttonText}>{locked?t('delivery_create_retry'):t('delivery_create_submit')}</Text></Pressable>
 {locked&&!busy&&!denied?<Text style={s.subtitle}>{t('delivery_create_retry_hint')}</Text>:null}
 </View><FlatList data={presets} keyExtractor={p=>p.id} contentContainerStyle={s.list} ListEmptyComponent={!busy?<Text style={s.empty}>{t('delivery_create_empty')}</Text>:null} renderItem={({item})=><View style={s.row}><View><Text style={s.name}>{item.name}</Text><Text style={s.meta}>{item.config.format.toUpperCase()}</Text></View><View>
 <Pressable accessibilityRole="checkbox" accessibilityLabel={`${item.name}: ${t('delivery_create_required')}`} accessibilityState={{checked:required.includes(item.id)}} disabled={!recovered||busy||locked||denied||(item.config.metadata_policy||'strip')!==metadataPolicy} onPress={()=>choose(item.id,'required')}><Text style={s.name}>{required.includes(item.id)?'☑':'☐'} {t('delivery_create_required')}</Text></Pressable>
 <Pressable accessibilityRole="checkbox" accessibilityLabel={`${item.name}: ${t('delivery_create_optional')}`} accessibilityState={{checked:optional.includes(item.id)}} disabled={!recovered||busy||locked||denied||(item.config.metadata_policy||'strip')!==metadataPolicy} onPress={()=>choose(item.id,'optional')}><Text style={s.name}>{optional.includes(item.id)?'☑':'☐'} {t('delivery_create_optional')}</Text></Pressable>
 </View></View>} ListFooterComponent={cursor&&!locked&&!denied?<Pressable accessibilityRole="button" disabled={busy} onPress={()=>void load(cursor)} style={s.button}><Text style={s.buttonText}>{t('delivery_create_more')}</Text></Pressable>:null}/></SafeAreaView>;
}
