/**
 * Implementation note.
 *
 * Implementation note.
 * Implementation note.
 */

declare module 'node:test' {
  export function describe(name: string, fn: () => void): void;
  export function it(name: string, fn: () => void | Promise<void>): void;
}

declare module 'node:assert/strict' {
  interface Assert {
    (value: unknown, message?: string): asserts value;
    equal(actual: unknown, expected: unknown, message?: string): void;
    notEqual(actual: unknown, expected: unknown, message?: string): void;
    deepEqual(actual: unknown, expected: unknown, message?: string): void;
    ok(value: unknown, message?: string): asserts value;
    match(value: string, pattern: RegExp, message?: string): void;
    throws(fn: () => unknown, message?: string): void;
    rejects(promise: Promise<unknown> | (() => Promise<unknown>), error?: RegExp, message?: string): Promise<void>;
  }
  const assert: Assert;
  export default assert;
}
