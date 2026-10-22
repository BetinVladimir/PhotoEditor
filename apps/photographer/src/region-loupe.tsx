import {Button,Text} from 'react-native-paper';
import {useEffect,useState} from 'react';
import {Image,View} from 'react-native';
import type {RegionDisplay} from './region-preview-display';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
export type RegionLoupeLoader=(signal:AbortSignal)=>Promise<RegionDisplay>;
type State={scope:string;load:RegionLoupeLoader;display?:RegionDisplay;failed?:boolean};
/** Loader owns admission, manifest proof and decoding; mount never requests a master. */
export function RegionLoupe({scope,load}:{scope:string;load:RegionLoupeLoader}) {
 const __uiT=useLocale().t;

 const {t}=useLocale(),[state,setState]=useState<State>(),[retry,setRetry]=useState(0);
 useEffect(()=>{
  const controller=new AbortController();let owned:RegionDisplay|undefined;
  setState({scope,load});
  void Promise.resolve().then(()=>{if(controller.signal.aborted)throw new Error('REGION_PREVIEW_ABORTED');return load(controller.signal);}).then(display=>{
   if(controller.signal.aborted){display.dispose();return;}
   owned=display;setState({scope,load,display});
  }).catch(()=>{if(!controller.signal.aborted)setState({scope,load,failed:true});});
  return()=>{controller.abort();owned?.dispose();};
 },[scope,load,retry]);
 const current=state?.scope===scope&&state.load===load?state:undefined,display=current?.display;
 return <View style={{gap:8}}><Text style={s.name}>{t('editor_loupe_title')}</Text>
  {current?.failed?<View><Text accessibilityRole="alert" style={s.error}>{t('editor_loupe_failed')}</Text><Button accessibilityRole="button" onPress={()=>setRetry(value=>value+1)}    mode="contained" contentStyle={{minHeight:48}}>{t('editor_loupe_retry')}</Button></View>:display?<>
   <Text style={s.meta}>{display.scale.sourcePixels?t('editor_loupe_source_detail'):__uiT('ui_ab8b18276ea7', {p0:(t('editor_loupe_rendered_scale')),p1:(Math.floor(display.scale.x*1000)/10),p2:(Math.floor(display.scale.y*1000)/10)})}</Text>
   <Image accessibilityLabel={t('editor_loupe_image')} source={{uri:display.uri}} resizeMode="contain" style={{width:'100%',aspectRatio:display.width/display.height,backgroundColor:'#202124'}}/>
  </>:<Text accessibilityLiveRegion="polite" style={s.meta}>{t('editor_loupe_loading')}</Text>}
 </View>;
}
