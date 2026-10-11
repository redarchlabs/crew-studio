import { describe, expect, it } from 'vitest'
import { matchPrice, stripProvider } from './pricing'
import { makePrice } from './testFixtures'

const prices = [
  makePrice({ id: 'p1', model: 'gpt-6-luna' }),
  makePrice({ id: 'p2', model: 'openai/gpt-6-luna' }),
  makePrice({ id: 'p3', model: 'gemini-2.5-flash' }),
]

describe('stripProvider', () => {
  it('removes the provider prefix only', () => {
    expect(stripProvider('openai/gpt-6-luna')).toBe('gpt-6-luna')
    expect(stripProvider('gpt-6-luna')).toBe('gpt-6-luna')
  })
})

describe('matchPrice', () => {
  it('prefers an exact match', () => {
    expect(matchPrice('openai/gpt-6-luna', prices)).toEqual({ price: prices[1], via: 'exact' })
  })

  it('falls back to the part after the provider prefix', () => {
    expect(matchPrice('gemini/gemini-2.5-flash', prices)).toEqual({ price: prices[2], via: 'unprefixed' })
  })

  it('matches unprefixed models exactly', () => {
    expect(matchPrice('gpt-6-luna', prices)?.price.id).toBe('p1')
  })

  it('returns null when nothing matches', () => {
    expect(matchPrice('anthropic/claude-x', prices)).toBeNull()
    expect(matchPrice('', prices)).toBeNull()
    expect(matchPrice('gpt-6-luna', [])).toBeNull()
  })
})
