import '@testing-library/jest-dom'

// Node 22+ has a localStorage of its own, which without --localstorage-file is
// unusable and shadows jsdom's. Tests get an in-memory one in its place.
class MemoryStorage implements Storage {
  private items = new Map<string, string>()
  get length() {
    return this.items.size
  }
  clear() {
    this.items.clear()
  }
  getItem(key: string) {
    return this.items.get(String(key)) ?? null
  }
  key(i: number) {
    return [...this.items.keys()][i] ?? null
  }
  removeItem(key: string) {
    this.items.delete(String(key))
  }
  setItem(key: string, value: string) {
    this.items.set(String(key), String(value))
  }
}

function usable(s: Storage | undefined): boolean {
  try {
    return typeof s?.clear === 'function' && typeof s.getItem === 'function'
  } catch {
    return false
  }
}

let current: Storage | undefined
try {
  current = globalThis.localStorage
} catch {
  current = undefined
}
if (!usable(current)) {
  const storage = new MemoryStorage()
  for (const target of new Set<object>([globalThis, window])) {
    Object.defineProperty(target, 'localStorage', { value: storage, configurable: true, writable: true })
  }
}
