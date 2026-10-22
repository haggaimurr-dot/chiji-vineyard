export interface ConfigChange { path: string; kind: 'added' | 'removed' | 'changed'; before?: unknown; after?: unknown }

// JSON Pointer paths keep dots, slashes and array indexes unambiguous.
export function configDiff(before: unknown, after: unknown, path = ''): ConfigChange[] {
  if (Object.is(before, after)) return []
  const object = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === 'object'
  if (object(before) && object(after) && Array.isArray(before) === Array.isArray(after)) {
    return [...new Set([...Object.keys(before), ...Object.keys(after)])].flatMap(key => {
      const next = `${path}/${key.replace(/~/g, '~0').replace(/\//g, '~1')}`
      if (!Object.hasOwn(before, key)) return [{ path: next, kind: 'added' as const, after: after[key] }]
      if (!Object.hasOwn(after, key)) return [{ path: next, kind: 'removed' as const, before: before[key] }]
      return configDiff(before[key], after[key], next)
    })
  }
  return [{ path: path || '/', kind: 'changed', before, after }]
}
