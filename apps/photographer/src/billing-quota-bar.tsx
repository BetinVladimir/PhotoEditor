import {ProgressBar, Text} from 'react-native-paper';
import {View} from 'react-native';
import {quotaProgress} from './billing-format';

/**
 * Quota progress bar. Progress is clamped to [0, 1]; when used exceeds granted,
 * an accessible overflow label is rendered instead of silently clipping the bar.
 * No PII is rendered or passed to analytics.
 */
export function QuotaBar({
  used, granted, label, overflowLabel,
}: {
  used: bigint;
  granted: bigint;
  label: string;
  overflowLabel: string;
}) {
  const progress = quotaProgress(used, granted);
  const overflow = used > granted && granted > 0n;
  return (
    <View>
      <ProgressBar
        progress={progress}
        accessible
        accessibilityLabel={label}
        accessibilityRole="progressbar"
        accessibilityValue={{min: 0, max: 1, now: progress}}
      />
      {overflow && (
        <Text
          accessibilityRole="alert"
          accessibilityLiveRegion="polite"
        >
          {overflowLabel}
        </Text>
      )}
    </View>
  );
}
