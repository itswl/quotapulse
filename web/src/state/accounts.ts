// The accounts meters are sampled from, credentials masked.
import { listAccounts } from '../api/endpoints';
import { resource } from './resource';

export const accounts = resource(listAccounts, ['meters', 'incidents', 'settings']);
