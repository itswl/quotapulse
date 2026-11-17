// Puts the answer of a write into the list at once, so the page does not
// wait for the event stream to come round. The stream's own refetch follows
// and confirms it.
import type { ReminderView } from '../../api/types';
import { reminders } from '../../state/reminders';
import { soonestFirst } from './words';

/** Adds a date to the list, or replaces the one with the same id. */
export function putReminder(next: ReminderView): void {
  reminders.set((list) => {
    if (!list) return list;
    const known = list.some((item) => item.id === next.id);
    return soonestFirst(known ? list.map((item) => (item.id === next.id ? next : item)) : [...list, next]);
  });
}

export function dropReminder(id: string): void {
  reminders.set((list) => list?.filter((item) => item.id !== id));
}
