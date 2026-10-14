import {useEffect,useState} from 'react';
import {ActivityIndicator,Pressable,Text,TextInput,View} from 'react-native';
import {selectRAWDevelopment,type RAWCapabilities,type RAWDevelopment} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

export function RAWUploadSettings({api,project,disabled,onChange}:{api:OwnerAPI;project:string;disabled:boolean;onChange:(policy:RAWDevelopment|undefined,valid:boolean)=>void}){
 const {t}=useLocale();
 const [catalogue,setCatalogue]=useState<RAWCapabilities>();
 const [failed,setFailed]=useState(false),[retry,setRetry]=useState(0),[explicit,setExplicit]=useState(false);
 const [demosaic,setDemosaic]=useState<RAWDevelopment['demosaic']>('ahd');
 const [wb,setWB]=useState<RAWDevelopment['wb_source']>('camera');
 const [highlight,setHighlight]=useState<RAWDevelopment['highlight_policy']>('clip');
 const [exposure,setExposure]=useState('0');
 useEffect(()=>{
  const controller=new AbortController();setCatalogue(undefined);setFailed(false);setExplicit(false);onChange(undefined,true);
  void api.rawCapabilities(project,controller.signal).then(value=>{if(!controller.signal.aborted)setCatalogue(value)}).catch(()=>{if(!controller.signal.aborted)setFailed(true)});
  return()=>controller.abort();
 },[api,project,retry,onChange]);
 let policy:RAWDevelopment|undefined;
 try{if(explicit&&catalogue&&exposure.trim()!==''&&/^-?(?:\d+(?:\.\d*)?|\.\d+)$/.test(exposure))policy=selectRAWDevelopment(catalogue,project,{demosaic,wb_source:wb,highlight_policy:highlight,exposure_ev:Number(exposure)});}catch{/* Invalid input remains visible; admission is disabled by parent. */}
 useEffect(()=>{onChange(explicit?policy:undefined,!explicit||!!policy)},[onChange,explicit,policy?.demosaic,policy?.wb_source,policy?.highlight_policy,policy?.exposure_ev,policy?.decoder_version]);
 return <View>
  <Text style={s.name}>{t('raw_development')}</Text>
  {!catalogue&&!failed?<ActivityIndicator accessibilityLabel={t('raw_loading')}/>:null}
  {failed?<><Text accessibilityRole="alert" style={s.error}>{t('raw_load_failed')}</Text><Pressable accessibilityRole="button" disabled={disabled} onPress={()=>setRetry(x=>x+1)}><Text>{t('raw_retry')}</Text></Pressable></>:null}
  {catalogue&&!catalogue.available?<Text style={s.meta}>{t('raw_unavailable')}</Text>:null}
  {catalogue?.available?<>
   <Pressable accessibilityRole="checkbox" accessibilityState={{checked:explicit,disabled}} disabled={disabled} onPress={()=>setExplicit(x=>!x)}><Text>{t('raw_custom')}</Text></Pressable>
   {explicit?<>
    <Text>{t('raw_demosaic')}</Text>{catalogue.development!.demosaic.map(value=><Pressable key={value} accessibilityRole="radio" accessibilityState={{checked:demosaic===value,disabled}} disabled={disabled} onPress={()=>setDemosaic(value)}><Text>{value}</Text></Pressable>)}
    <Text>{t('raw_wb')}</Text>{catalogue.development!.wb_source.map(value=><Pressable key={value} accessibilityRole="radio" accessibilityState={{checked:wb===value,disabled}} disabled={disabled} onPress={()=>setWB(value)}><Text>{t(value==='camera'?'raw_camera':value==='daylight'?'raw_daylight':'raw_auto')}</Text></Pressable>)}
    <Text>{t('raw_highlights')}</Text>{catalogue.development!.highlight_policy.map(value=><Pressable key={value} accessibilityRole="radio" accessibilityState={{checked:highlight===value,disabled}} disabled={disabled} onPress={()=>setHighlight(value)}><Text>{t(value==='clip'?'raw_clip':value==='blend'?'raw_blend':'raw_reconstruct')}</Text></Pressable>)}
    <TextInput accessibilityLabel={t('raw_exposure')} value={exposure} editable={!disabled} onChangeText={setExposure} keyboardType="numbers-and-punctuation"/>
    {!policy?<Text accessibilityRole="alert" style={s.error}>{t('raw_exposure_invalid')}</Text>:null}
    <Text style={s.meta}>{t('raw_queue_hint')}</Text>
   </>:null}
  </>:null}
 </View>;
}
