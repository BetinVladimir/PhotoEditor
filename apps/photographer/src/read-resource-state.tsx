import {Button} from 'react-native-paper';
import { ActivityIndicator, Text, TouchableRipple as Pressable } from 'react-native-paper';
import { View } from 'react-native';
import { useLocale } from './locale-context';
import type { ReadPhase } from './use-read-resource';
export function ReadResourceState({ phase, retry, cancel }: {
    phase: ReadPhase;
    retry: () => void;
    cancel: () => void;
}) {
    const { t } = useLocale();
    if (phase === 'ready')
        return null;
    return <View style={{ padding: 16, gap: 12 }}>{phase === 'loading' ? <><ActivityIndicator accessibilityLabel={t('read_loading')}/><Button key="read-cancel" accessibilityRole="button" onPress={cancel} mode="text">{t('read_cancel')}</Button></> : <><Text style={{ color: "#eeeeee" }} accessibilityRole={phase === 'error' ? 'alert' : undefined}>{t(phase === 'error' ? 'read_error' : phase === 'paused' ? 'read_paused' : 'read_cancelled')}</Text>{phase !== 'paused' ? <Button key="read-retry" accessibilityRole="button" onPress={retry} mode="text">{t('read_retry')}</Button> : null}</>}</View>;
}
