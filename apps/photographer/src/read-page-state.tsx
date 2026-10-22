import {Button} from 'react-native-paper';
import { Text, TouchableRipple as Pressable } from 'react-native-paper';
import { View } from 'react-native';
import { useLocale } from './locale-context';
import { ReadResourceState } from './read-resource-state';
export function ReadPageState({ hasMore, morePhase, more, cancelMore, disabled = false }: {
    hasMore: boolean;
    morePhase: 'idle' | 'loading' | 'error' | 'cancelled';
    more: () => Promise<void>;
    cancelMore: () => void;
    disabled?: boolean;
}) {
    const { t } = useLocale();
    if (!hasMore)
        return null;
    return <View style={{ padding: 16 }}>{morePhase === 'idle' ? <Button accessibilityRole="button" disabled={disabled} onPress={() => void more()} mode="text">{t('read_more')}</Button> : <ReadResourceState phase={morePhase} retry={() => { if (!disabled)
        void more(); }} cancel={cancelMore}/>}</View>;
}
