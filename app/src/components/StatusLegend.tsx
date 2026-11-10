import React from 'react';
import { Text, View } from 'react-native';
import { colors } from '../theme/colors';
import { makeStyles, SIDE } from '../lib/scale';
import { DAY_STYLE, DayStatus } from '../lib/dayStatus';

const ORDER: DayStatus[] = ['COMPLETE', 'IN_PROGRESS', 'INCOMPLETE', 'NONE'];

/** Tiny key for the day chip colours. */
export function StatusLegend() {
  const styles = useStyles();
  return (
    <View
      style={styles.row}
      accessible
      accessibilityLabel="Colours: green complete, amber in progress, red incomplete, grey no record"
    >
      {ORDER.map((st) => (
        <View key={st} style={styles.item}>
          <View style={[styles.swatch, { backgroundColor: DAY_STYLE[st].bg }]} />
          <Text style={styles.text}>{DAY_STYLE[st].word.toUpperCase()}</Text>
        </View>
      ))}
    </View>
  );
}

const useStyles = makeStyles((s) => ({
  row: { flexDirection: 'row', flexWrap: 'wrap', marginHorizontal: s(SIDE), marginBottom: s(24) },
  item: { width: '50%', flexDirection: 'row', alignItems: 'center', marginBottom: s(10) },
  swatch: { width: s(34), height: s(34), borderRadius: s(7), marginRight: s(12) },
  text: { color: colors.navyHeader, fontSize: s(30), fontWeight: '700' },
}));
