import type { ModelPrice } from '../api/types'

export type PriceMatch = { price: ModelPrice; via: 'exact' | 'unprefixed' }

/** Strips a LiteLLM provider prefix: "openai/gpt-6-luna" -> "gpt-6-luna". */
export function stripProvider(model: string): string {
  const slash = model.indexOf('/')
  return slash >= 0 ? model.slice(slash + 1) : model
}

/**
 * Finds the price entry for an LLM model id, following api.md:
 * exact match first, then the part after the provider prefix.
 */
export function matchPrice(model: string, prices: readonly ModelPrice[]): PriceMatch | null {
  const wanted = model.trim()
  if (!wanted) return null
  const exact = prices.find((p) => p.model === wanted)
  if (exact) return { price: exact, via: 'exact' }
  const bare = stripProvider(wanted)
  if (bare === wanted || !bare) return null
  const unprefixed = prices.find((p) => p.model === bare)
  return unprefixed ? { price: unprefixed, via: 'unprefixed' } : null
}
