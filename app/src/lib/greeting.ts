/** 05:00-11:59 morning, 12:00-16:59 afternoon, otherwise evening. Uses the device clock. */
export function greeting(date: Date = new Date()): string {
  const h = date.getHours();
  if (h >= 5 && h < 12) return 'GOOD MORNING';
  if (h >= 12 && h < 17) return 'GOOD AFTERNOON';
  return 'GOOD EVENING';
}
