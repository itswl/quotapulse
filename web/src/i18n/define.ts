/**
 * Declares one area's words. English is the reference; Chinese must have
 * exactly the same keys, or the file does not compile.
 *
 *   export default defineArea({
 *     en: { title: 'Dates', 'empty.body': 'Nothing is coming up.' },
 *     zh: { title: '日期', 'empty.body': '近期没有要到的日子。' },
 *   });
 */
export function defineArea<const K extends string>(words: {
  en: Record<K, string>;
  zh: Record<NoInfer<K>, string>;
}): { en: Record<K, string>; zh: Record<K, string> } {
  return words;
}
