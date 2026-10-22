import {TouchableRipple,Button,Text} from 'react-native-paper';
import {useState} from 'react';
import {Image,View} from 'react-native';
import {compareFrameTransform,decideCompareFrame,selectCompareFrame,setCompareViewport,type FrameComparison} from './compare-frames';
import {useCommands} from './use-commands';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
function Frame({uri,viewport,selected,onSelect,label}:{uri:string;viewport:FrameComparison['viewport'];selected:boolean;onSelect():void;label:string}){
 const [size,setSize]=useState({width:0,height:0});
 const transform=size.width>0&&size.height>0?compareFrameTransform(viewport,size.width,size.height):{scale:viewport.scale,translateX:0,translateY:0};
 return <TouchableRipple accessibilityRole="button" accessibilityLabel={label} accessibilityState={{selected}} onPress={onSelect} style={{flex:1,minWidth:120,borderWidth:2,borderColor:selected?'#d0bcff':'#444'}}><View style={{alignSelf:'stretch'}}>
  <View onLayout={event=>setSize(event.nativeEvent.layout)} style={{aspectRatio:1,overflow:'hidden',backgroundColor:'#202124'}}><Image source={{uri}} accessibilityLabel={label} resizeMode="contain" style={{width:'100%',height:'100%',transform:[{translateX:transform.translateX},{translateY:transform.translateY},{scale:transform.scale}]}}/></View>
 </View></TouchableRipple>;
}
/** Controlled session state; parent resets it when authorized selection/scope changes. */
export function CompareFramesView({state,onChange}:{state:FrameComparison;onChange(state:FrameComparison):void}){
 const __uiT=useLocale().t;

 const {t}=useLocale();
 const previous=()=>onChange(selectCompareFrame(state,-1)),next=()=>onChange(selectCompareFrame(state,1));
 const accept=()=>onChange(decideCompareFrame(state,'accept')),reject=()=>onChange(decideCompareFrame(state,'reject'));
 useCommands({'compare.previous':previous,'compare.next':next,'compare.accept':accept,'compare.reject':reject});
 const move=(x:number,y:number)=>onChange(setCompareViewport(state,{...state.viewport,centerX:Math.min(1,Math.max(0,state.viewport.centerX+x)),centerY:Math.min(1,Math.max(0,state.viewport.centerY+y))}));
 const zoom=(factor:number)=>onChange(setCompareViewport(state,{...state.viewport,scale:Math.min(16,Math.max(1,state.viewport.scale*factor))}));
 const actions=[['compare_previous',previous],['compare_next',next],['compare_accept',accept],['compare_reject',reject],['compare_zoom_in',()=>zoom(2)],['compare_zoom_out',()=>zoom(.5)],['compare_pan_left',()=>move(-.1,0)],['compare_pan_right',()=>move(.1,0)],['compare_pan_up',()=>move(0,-.1)],['compare_pan_down',()=>move(0,.1)]] as const;
 return <View style={{gap:8}}><Text style={s.name}>{t('compare_title')}</Text><View style={{flexDirection:'row',flexWrap:'wrap',gap:8}}>{state.frames.map((frame,index)=><View key={frame.versionId} style={{flex:1,minWidth:120}}><Frame uri={frame.previewURI} viewport={state.viewport} selected={index===state.selected} onSelect={()=>onChange({...state,selected:index})} label={__uiT('ui_8cbaa55a52ef', {p0:(t('compare_frame')),p1:(index+1)})}/><Text style={s.meta}>{state.decisions[frame.versionId]?t(state.decisions[frame.versionId]==='accept'?'compare_accepted':'compare_rejected'):t('compare_undecided')}</Text></View>)}</View><View style={{flexDirection:'row',flexWrap:'wrap',gap:8}}>{actions.map(([key,action])=><Button key={key} accessibilityRole="button" accessibilityLabel={t(key)} onPress={action}    mode="contained" contentStyle={{minHeight:48}}>{t(key)}</Button>)}</View></View>;
}
