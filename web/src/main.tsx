// Boot: styles, theme, the first session check, and the app itself.
import { render } from 'preact';
import { App } from './app';
import { checkSession } from './state/session';
import './styles/index.css';
import './theme';

void checkSession();

const root = document.getElementById('app');
if (root) render(<App />, root);
