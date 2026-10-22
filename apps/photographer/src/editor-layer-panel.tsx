import {Button,TouchableRipple,Text} from 'react-native-paper';
import {useState} from 'react';
import {View} from 'react-native';
import type {V2VersionBinding} from './catalog-types';
import {currentEditorSnapshot,type EditorState} from './editor-store';
import {patchLayerCommand,moveLayerCommand} from './editor-layer-commands';
import {useLocale} from './locale-context';
import {catalogStyle as s} from './catalog-style';
/** Controlled panel; every edit returns the existing editor's full source-bound history state. */
export function EditorLayerPanel({state,binding,onChange,disabled=false}:{state:EditorState;binding:V2VersionBinding;onChange:(state:EditorState)=>void;disabled?:boolean}){
 const {t}=useLocale();const [invalid,setInvalid]=useState(false);const stack=currentEditorSnapshot(state).layerStack;
 function change(action:()=>EditorState){try{onChange(action());setInvalid(false);}catch{setInvalid(true);}}
 return <View style={{gap:8}}><Text style={s.name}>{t('layers_title')}</Text>{invalid?<Text accessibilityRole="alert" style={s.error}>{t('layers_invalid')}</Text>:null}
  {!stack?.layers.length?<Text style={s.meta}>{t('layers_empty')}</Text>:stack.layers.map((layer,index)=><View key={layer.id} style={{padding:8,gap:6}}><Text style={s.meta}>{t('layers_layer')} {index+1}</Text>
   <Button accessibilityRole="button" disabled={disabled} onPress={()=>change(()=>patchLayerCommand(state,binding,layer.id,{enabled:!layer.enabled}))}    mode="contained" contentStyle={{minHeight:48}}>{t(layer.enabled?'layers_disable':'layers_enable')}</Button>
   <Text style={s.meta}>{t('layers_opacity')}: {Math.round(layer.opacity*100)}%</Text><View style={{flexDirection:'row',gap:6}}>{[-.1,.1].map(delta=><Button key={delta} accessibilityRole="button" accessibilityLabel={t(delta<0?'layers_opacity_down':'layers_opacity_up')} disabled={disabled} onPress={()=>change(()=>patchLayerCommand(state,binding,layer.id,{opacity:Math.min(1,Math.max(0,Math.round((layer.opacity+delta)*100)/100))}))}    mode="contained" contentStyle={{minHeight:48}}>{delta<0?'−':'+'}</Button>)}</View>
   <View style={{flexDirection:'row',gap:6}}>{(['normal','multiply','screen'] as const).map(blend=><TouchableRipple key={blend} accessibilityRole="button" accessibilityState={{selected:layer.blend===blend}} disabled={disabled} onPress={()=>change(()=>patchLayerCommand(state,binding,layer.id,{blend}))} style={s.button}><View style={{alignItems:'center',justifyContent:'center'}}><Text style={s.buttonText}>{t(`layers_blend_${blend}`)}</Text></View></TouchableRipple>)}</View>
   <View style={{flexDirection:'row',gap:6}}>{[-1,1].map(direction=><Button key={direction} accessibilityRole="button" disabled={disabled||index+direction<0||index+direction>=stack.layers.length} onPress={()=>change(()=>moveLayerCommand(state,binding,layer.id,index+direction))}    mode="contained" contentStyle={{minHeight:48}}>{t(direction<0?'layers_move_up':'layers_move_down')}</Button>)}</View>
  </View>)}
 </View>;
}
