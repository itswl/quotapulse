// Every date (the API calls them reminders), soonest first.
import { listReminders } from '../api/endpoints';
import { resource } from './resource';

export const reminders = resource(listReminders, ['reminders', 'incidents', 'settings']);
