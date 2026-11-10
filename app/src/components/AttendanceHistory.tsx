import React from 'react';
import { FlatList } from 'react-native';
import { colors } from '../theme/colors';
import { makeStyles } from '../lib/scale';
import { Week } from '../lib/groupByWeek';
import { WeekCard } from './WeekCard';

type Props = { weeks: Week[]; refreshing?: boolean; onRefresh?: () => void };

export function AttendanceHistory({ weeks, refreshing = false, onRefresh }: Props) {
  const styles = useStyles();
  return (
    <FlatList
      style={styles.list}
      contentContainerStyle={styles.content}
      data={weeks}
      keyExtractor={(w) => w.monday.toISOString()}
      renderItem={({ item }) => <WeekCard week={item} />}
      refreshing={refreshing}
      onRefresh={onRefresh}
    />
  );
}

const useStyles = makeStyles((s) => ({
  list: { flex: 1, backgroundColor: colors.historyBg },
  content: { paddingTop: s(44), paddingBottom: s(20) },
}));
