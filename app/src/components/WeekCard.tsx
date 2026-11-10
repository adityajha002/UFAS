import React, { useState } from 'react';
import { Text, View } from 'react-native';
import { colors } from '../theme/colors';
import { chipWidth, makeStyles, SIDE, useLayout, WEEK_PAD } from '../lib/scale';
import { DayInfo, Week } from '../lib/groupByWeek';
import { weekCaption, weekRange } from '../lib/weekLabel';
import { dayDetailText } from '../lib/dayStatus';
import { DayChip } from './DayChip';

export function WeekCard({ week }: { week: Week }) {
  const styles = useStyles();
  const { s, width } = useLayout();
  const chipW = chipWidth(s, width);
  const c = weekCaption(week.monday);
  const [selected, setSelected] = useState<string | null>(null); // tapped day: shows its status and hours
  const chip = (d: DayInfo) => (
    <DayChip
      key={d.date}
      label={d.label}
      detail={d}
      width={chipW}
      isToday={d.isToday}
      selected={selected === d.date}
      onPress={() => setSelected((cur) => (cur === d.date ? null : d.date))}
    />
  );
  const sel = week.days.find((d) => d.date === selected);
  return (
    <View style={styles.card}>
      <View style={styles.row}>{week.days.slice(0, 3).map(chip)}</View>
      <View style={styles.row}>{week.days.slice(3).map(chip)}</View>
      {sel ? (
        <Text style={styles.detail} accessibilityLiveRegion="polite">{dayDetailText(sel.label, sel.date, sel)}</Text>
      ) : null}
      <View style={styles.footer}>
        <Text style={styles.foot}>{weekRange(week.monday)}</Text>
        <Text style={styles.foot}>{c.month} - {c.n}<Text style={styles.sup}>{c.suffix}</Text> WEEK</Text>
      </View>
    </View>
  );
}

const useStyles = makeStyles((s) => ({
  card: { marginHorizontal: s(SIDE), backgroundColor: colors.navyCard, borderRadius: s(44), paddingTop: s(30), paddingHorizontal: s(WEEK_PAD), marginBottom: s(40), overflow: 'hidden' },
  row: { flexDirection: 'row', justifyContent: 'center' },
  detail: { color: colors.white, fontSize: s(34), fontWeight: '700', textAlign: 'center', marginTop: s(12) },
  footer: { flexDirection: 'row', justifyContent: 'space-between', paddingHorizontal: s(10), paddingVertical: s(28), marginTop: s(16), borderTopWidth: 1, borderTopColor: '#6E6A9A' },
  foot: { color: colors.white, fontSize: s(32) },
  sup: { fontSize: s(20) },
}));
