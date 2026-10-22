import {useLocale} from './locale-context';
import {Text} from 'react-native-paper';
import {Image,View} from 'react-native';import{catalogStyle as s}from"./catalog-style";
export function CompareCanvas({before,after}:{before:string;after:string}){
 const __uiT=useLocale().t;
return <View accessible accessibilityLabel={__uiT('ui_f7774ef014fa')} style={{gap:8}}><Text style={s.name}>{__uiT('ui_97b2c9325c55')}</Text><View style={{flexDirection:"row",gap:8}}><View style={{flex:1}}><Text style={s.meta}>{__uiT('ui_9bb725005055')}</Text><Image source={{uri:before}} resizeMode="contain" style={{width:"100%",aspectRatio:1,borderRadius:10,backgroundColor:"#202124"}}/></View><View style={{flex:1}}><Text style={s.meta}>{__uiT('ui_b2452d1eba32')}</Text><Image source={{uri:after}} resizeMode="contain" style={{width:"100%",aspectRatio:1,borderRadius:10,backgroundColor:"#202124"}}/></View></View></View>}
