import {useLocale} from './locale-context';
import {Text} from 'react-native-paper';
import {useEffect,useMemo,useRef} from 'react';
import {Image,View} from 'react-native';

type Display={uri:string;width:number;height:number};
type Props={images:readonly Display[];onViewed:(ready:boolean)=>void;onError?:()=>void};

// Callers provide hash-verified, decoded and source-bound display bytes.
// Decode readiness alone does not prove that either image was displayed.
export function VerifiedReviewComparison({images,onViewed,onError}:Props){
 const __uiT=useLocale().t;

 const token=useMemo(()=>({loaded:new Set<number>()}),[images]);
 const current=useRef<typeof token|null>(token);current.current=token;
 useEffect(()=>{onViewed(false);return()=>{if(current.current===token)current.current=null}},[token,onViewed]);
 function update(index:number,loaded:boolean){
  if(current.current!==token)return;
  if(loaded)token.loaded.add(index);else token.loaded.delete(index);
  onViewed(images.length===2&&token.loaded.has(0)&&token.loaded.has(1));
 }
 return <View>{images.map((image,index)=><View key={`${index}:${image.uri}`}>
  <Text>{index===0?__uiT('ui_9bb725005055'):__uiT('ui_7b68fe551085')}</Text>
  <Image accessibilityLabel={index===0?__uiT('ui_201c26954a24'):__uiT('ui_a6b5ec20f096')} source={{uri:image.uri}}
   onLoad={()=>update(index,true)} onError={()=>{if(current.current!==token)return;update(index,false);onError?.();}} resizeMode="contain"
   style={{width:'100%',aspectRatio:image.width/image.height}}/>
 </View>)}</View>;
}
