/* Side-effect style imports: esbuild bundles the CSS; tsc only needs to
   accept the import (TypeScript 7 raises TS2882 without a declaration). */

declare module '*.css';
