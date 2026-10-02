import {useLocale} from "./locale-context";
import {Pressable,Text,TextInput,View} from 'react-native';
import type {CaptureShotRequirement,CapturePerson} from './capture-types';
import {catalogStyle as s} from './catalog-style';
export function CaptureShotEditor({items,people,onChange,disabled}:{people:CapturePerson[];items:CaptureShotRequirement[];onChange:(items:CaptureShotRequirement[])=>void;disabled:boolean}){
 const {t}=useLocale();
 function change(id:string,patch:Partial<CaptureShotRequirement>){onChange(items.map(item=>item.id===id?{...item,...patch}:item))}
 return <View>{items.map(item=><View key={item.id} style={{marginVertical:8}}>
  <TextInput accessibilityLabel={t("capture_label")} editable={!disabled} style={s.input} value={item.label} maxLength={200} onChangeText={label=>change(item.id,{label})}/>
  <Text style={s.meta}>{t("capture_frames")}: {item.min_confirmed_frames}</Text>
  <View style={s.toolbar}>{[-1,1].map(delta=><Pressable key={delta} accessibilityRole="button" accessibilityLabel={delta<0?t("capture_frames_down"):t("capture_frames_up")} disabled={disabled||item.min_confirmed_frames+delta<1||item.min_confirmed_frames+delta>100} style={s.button} onPress={()=>change(item.id,{min_confirmed_frames:item.min_confirmed_frames+delta})}><Text style={s.buttonText}>{delta<0?'−':'+'}</Text></Pressable>)}</View>
  <Text style={s.meta}>{t("capture_quality")}: {Math.round(item.min_quality*100)}%</Text>
  <View style={s.toolbar}>{[-.05,.05].map(delta=><Pressable key={delta} accessibilityRole="button" accessibilityLabel={delta<0?t("capture_quality_down"):t("capture_quality_up")} disabled={disabled} style={s.button} onPress={()=>change(item.id,{min_quality:Math.max(0,Math.min(1,Math.round((item.min_quality+delta)*100)/100))})}><Text style={s.buttonText}>{delta<0?'−':'+'}</Text></Pressable>)}</View>
  <Text style={s.meta}>{t("capture_people")}: {item.person_ids.length}</Text>
  {item.person_ids.filter(id=>!people.some(person=>person.id===id&&person.state==='active')).map(id=><View key={id}><Text style={s.error}>{t("capture_unavailable")}</Text><Pressable accessibilityRole="button" disabled={disabled} style={s.button} onPress={()=>change(item.id,{person_ids:item.person_ids.filter(other=>other!==id)})}><Text style={s.buttonText}>{t("capture_remove_binding")}</Text></Pressable></View>)}

  {people.filter(person=>person.state==='active').map(person=><Pressable key={person.id} accessibilityRole="checkbox" accessibilityState={{checked:item.person_ids.includes(person.id),disabled}} disabled={disabled||(!item.person_ids.includes(person.id)&&item.person_ids.length>=100)} style={s.button} onPress={()=>change(item.id,{person_ids:item.person_ids.includes(person.id)?item.person_ids.filter(id=>id!==person.id):[...item.person_ids,person.id]})}><Text style={s.buttonText}>{item.person_ids.includes(person.id)?'✓ ':''}{person.label}</Text></Pressable>)}

  <Pressable accessibilityRole="button" disabled={disabled} style={s.button} onPress={()=>onChange(items.filter(other=>other.id!==item.id))}><Text style={s.buttonText}>{t("capture_remove")}</Text></Pressable>
 </View>)}
 <Pressable accessibilityRole="button" disabled={disabled||items.length>=200} style={s.button} onPress={()=>onChange([...items,{id:crypto.randomUUID(),label:t("capture_new"),person_ids:[],min_confirmed_frames:1,min_quality:.7}])}><Text style={s.buttonText}>{t("capture_add")}</Text></Pressable>
 </View>;
}
