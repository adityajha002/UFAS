import React from 'react';
import { Pressable, Text } from 'react-native';
import { colors } from '../theme/colors';
import { CHIP_GAP, makeStyles } from '../lib/scale';
import { DAY_STYLE, DayDetail, workedText } from '../lib/dayStatus';

const FULL: Record<string, string> = { MON: 'Monday', TUES: 'Tuesday', WEDS: 'Wednesday', THU: 'Thursday', FRI: 'Friday' };

type Props = {
  label: string;
  detail: DayDetail;
  /** Width in dp, so all chips in a card match. */
  width: number;
  isToday?: boolean;
  selected?: boolean;
  onPress?: () => void;
};

/** Green complete, amber in progress, red incomplete, grey no record. Tap to show the day's details. */
export function DayChip({ label, detail, width, isToday = false, selected = false, onPress }: Props) {
  const styles = useStyles();
  const st = DAY_STYLE[detail.status];
  const hours = workedText(detail);
  return (
    <Pressable
      onPress={onPress}
      disabled={!onPress}
      style={[styles.chip, { width, backgroundColor: st.bg }, isToday && styles.today, selected && styles.selected]}
      accessibilityRole={onPress ? 'button' : undefined}
      accessibilityState={onPress ? { selected } : undefined}
      accessibilityLabel={`${FULL[label]}${isToday ? ' (today)' : ''}, ${st.word}${hours ? `, worked ${hours}` : ''}`}
    >
      <Text style={styles.text}>{st.mark}{label}</Text>
    </Pressable>
  );
}

const useStyles = makeStyles((s) => ({
  chip: { height: s(100), borderRadius: s(14), alignItems: 'center', justifyContent: 'center', margin: s(CHIP_GAP) },
  today: { borderWidth: s(6), borderColor: colors.yellow },
  selected: { borderWidth: s(6), borderColor: colors.white },
  text: { fontSize: s(38), fontWeight: '800', color: colors.textDark },
}));
