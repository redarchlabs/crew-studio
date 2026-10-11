/** A URL from an outside system (such as a tracker) as a link target, only when it is http(s); otherwise undefined. */
export function safeHref(url: string | undefined | null): string | undefined {
  if (!url) return undefined
  try {
    const parsed = new URL(url)
    return parsed.protocol === 'https:' || parsed.protocol === 'http:' ? parsed.href : undefined
  } catch {
    return undefined
  }
}
