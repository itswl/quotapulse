// The provider definitions an account can be added for: the built-in
// catalog plus any saved from the dashboard.
import { listProviders } from '../api/endpoints';
import { resource } from './resource';

export const providers = resource(listProviders, ['settings']);
