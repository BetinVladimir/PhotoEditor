import {ActivityIndicator,Button,Text,TextInput,TouchableRipple} from 'react-native-paper';
import {APIError} from '@photoeditor/api-client';
import {useEffect,useState} from 'react';
import {View} from 'react-native';
import {selectRAWDevelopment,type RAWCapabilities,type RAWDevelopment} from '@photoeditor/api-client';
import type {OwnerAPI} from './owner-api';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';

export function RAWUploadSettings({api,project,disabled,onChange,onAuthorityLost}:{api:OwnerAPI;project:string;disabled:boolean;onChange:(policy:RAWDevelopment|undefined,valid:boolean)=>void;onAuthorityLost?:()=>void}){
 const {t}=useLocale();
 const [catalogue,setCatalogue]=useState<RAWCapabilities>();
 const [failed,setFailed]=useState(false),[retry,setRetry]=useState(0),[explicit,setExplicit]=useState(false);
 const [demosaic,setDemosaic]=useState<RAWDevelopment['demosaic']>('ahd');
 const [wb,setWB]=useState<RAWDevelopment['wb_source']>('camera');
 const [highlight,setHighlight]=useState<RAWDevelopment['highlight_policy']>('clip');
 const [exposure,setExposure]=useState('0');
 useEffect(()=>{
  const controller=new AbortController();setCatalogue(undefined);setFailed(false);setExplicit(false);onChange(undefined,true);
  void api.rawCapabilities(project,controller.signal).then(value=>{if(!controller.signal.aborted)setCatalogue(value)}).catch(error=>{if(!controller.signal.aborted){if(error instanceof APIError&&[401,403,404].includes(error.status))onAuthorityLost?.();setFailed(true)}});
  return()=>controller.abort();
 },[api,project,retry,onChange,onAuthorityLost]);
 let policy:RAWDevelopment|undefined;
 try{if(explicit&&catalogue&&exposure.trim()!==''&&/^-?(?:\d+(?:\.\d*)?|\.\d+)$/.test(exposure))policy=selectRAWDevelopment(catalogue,project,{demosaic,wb_source:wb,highlight_policy:highlight,exposure_ev:Number(exposure)});}catch{/* Invalid input remains visible; admission is disabled by parent. */}
 useEffect(()=>{onChange(explicit?policy:undefined,!explicit||!!policy)},[onChange,explicit,policy?.demosaic,policy?.wb_source,policy?.highlight_policy,policy?.exposure_ev,policy?.decoder_version]);
 return <View>
  <Text style={s.name}>{t('raw_development')}</Text>
  {!catalogue&&!failed?<ActivityIndicator accessibilityLabel={t('raw_loading')}/>:null}
  {failed?<><Text accessibilityRole="alert" style={s.error}>{t('raw_load_failed')}</Text><Button accessibilityRole="button" disabled={disabled} onPress={()=>setRetry(x=>x+1)}>{t('raw_retry')}</Button></>:null}
  {catalogue&&!catalogue.available?<Text style={s.meta}>{t('raw_unavailable')}</Text>:null}
  {catalogue?.available?<>
   <TouchableRipple accessibilityRole="checkbox" accessibilityState={{checked:explicit,disabled}} aria-checked={explicit} disabled={disabled} onPress={()=>setExplicit(x=>!x)}><Text>{t('raw_custom')}</Text></TouchableRipple>
   {explicit?<>
    <Text>{t('raw_demosaic')}</Text>{catalogue.development!.demosaic.map(value=><TouchableRipple key={value} accessibilityRole="radio" accessibilityState={{checked:demosaic===value,disabled}} aria-checked={demosaic===value} disabled={disabled} onPress={()=>setDemosaic(value)}><Text>{value}</Text></TouchableRipple>)}
    <Text>{t('raw_wb')}</Text>{catalogue.development!.wb_source.map(value=><TouchableRipple key={value} accessibilityRole="radio" accessibilityState={{checked:wb===value,disabled}} aria-checked={wb===value} disabled={disabled} onPress={()=>setWB(value)}><Text>{t(value==='camera'?'raw_camera':value==='daylight'?'raw_daylight':'raw_auto')}</Text></TouchableRipple>)}
    <Text>{t('raw_highlights')}</Text>{catalogue.development!.highlight_policy.map(value=><TouchableRipple key={value} accessibilityRole="radio" accessibilityState={{checked:highlight===value,disabled}} aria-checked={highlight===value} disabled={disabled} onPress={()=>setHighlight(value)}><Text>{t(value==='clip'?'raw_clip':value==='blend'?'raw_blend':'raw_reconstruct')}</Text></TouchableRipple>)}
    <TextInput accessibilityLabel={t('raw_exposure')} value={exposure} editable={!disabled} onChangeText={setExposure} keyboardType="numbers-and-punctuation"/>
    {!policy?<Text accessibilityRole="alert" style={s.error}>{t('raw_exposure_invalid')}</Text>:null}
    <Text style={s.meta}>{t('raw_queue_hint')}</Text>
   </>:null}
  </>:null}
 </View>;
}
