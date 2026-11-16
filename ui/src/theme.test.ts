import { describe, it, expect } from 'vitest'
import { light, dark } from './theme'

describe('theme tokens', () => {
  it('uses the documented accent-text and dark background', () => {
    expect(light.accentText).toBe('#0f9488')
    expect(dark.bg).toBe('#282a36')
  })

  it('defines every semantic color in both themes', () => {
    for (const key of ['running', 'waiting', 'idle', 'done', 'danger'] as const) {
      expect(light[key]).toBeTruthy()
      expect(dark[key]).toBeTruthy()
    }
  })
})
