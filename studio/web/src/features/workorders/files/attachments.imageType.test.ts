import { describe, expect, it } from 'vitest'
import { isImageType } from './attachments'

describe('isImageType', () => {
  // `in` would also match Object.prototype keys
  it('does not treat Object.prototype property names as image types', () => {
    expect(isImageType('constructor')).toBe(false)
    expect(isImageType('toString')).toBe(false)
  })

  it('recognises real image types', () => {
    expect(isImageType('image/png')).toBe(true)
    expect(isImageType('text/plain')).toBe(false)
  })
})
