// Ready-to-use snippets shown after a trigger token is created or rotated, and callback verification examples.

type Json = string | number | boolean | null | Json[] | { [key: string]: Json }

const isIndex = (segment: string) => /^\d+$/.test(segment)

/** Writes `value` at a dotted path, creating objects (or arrays for numeric segments). Returns a new tree. */
function setPath(tree: Json, segments: readonly string[], value: Json): Json {
  if (segments.length === 0) return value
  const [head, ...rest] = segments
  if (isIndex(head)) {
    const arr = Array.isArray(tree) ? [...tree] : []
    const idx = Number(head)
    while (arr.length < idx) arr.push(null)
    arr[idx] = setPath(arr[idx] ?? null, rest, value)
    return arr
  }
  const obj = tree !== null && typeof tree === 'object' && !Array.isArray(tree) ? { ...tree } : {}
  obj[head] = setPath(obj[head] ?? null, rest, value)
  return obj
}

/**
 * A sample JSON body that fills every mapped input: { company: "customer.name" } -> { customer: { name: "<company>" } }.
 * Empty paths are skipped; with no mapping the sample is a small generic event.
 */
export function samplePayload(inputMap: Readonly<Record<string, string>>): Json {
  const entries = Object.entries(inputMap).filter(([, path]) => path.trim() !== '')
  if (entries.length === 0) return { event: 'example', message: 'Anything JSON up to 1 MB' }
  return entries.reduce<Json>((tree, [key, path]) => setPath(tree, path.trim().split('.').filter(Boolean), `<${key}>`), {})
}

/** Quotes a string for a POSIX shell using single quotes. */
export function shellQuote(text: string): string {
  return `'${text.replace(/'/g, `'\\''`)}'`
}

/** curl command that fires the trigger. */
export function curlExample(url: string, token: string, payload: Json, idempotencyKey: string): string {
  return [
    `curl -X POST ${shellQuote(url)}`,
    `  -H ${shellQuote(`Authorization: Bearer ${token}`)}`,
    `  -H 'Content-Type: application/json'`,
    `  -H ${shellQuote(`Idempotency-Key: ${idempotencyKey}`)}`,
    `  --data ${shellQuote(JSON.stringify(payload))}`,
  ].join(' \\\n')
}

/** A readable unique-ish key for examples ("order-1234-abcd"); real callers should use their own event id. */
export function exampleIdempotencyKey(random: () => number = Math.random): string {
  const part = () => Math.floor(random() * 0x10000).toString(16).padStart(4, '0')
  return `event-${part()}${part()}`
}

export const NODE_VERIFY = `import crypto from 'node:crypto'

// rawBody: the request body exactly as received (Buffer or string)
export function verifyCrewStudio(rawBody, headers, secret) {
  const timestamp = headers['x-crewstudio-timestamp']
  const signature = headers['x-crewstudio-signature'] ?? ''
  if (Math.abs(Date.now() / 1000 - Number(timestamp)) > 300) return false // older than 5 minutes
  const expected = 'sha256=' + crypto.createHmac('sha256', secret).update(\`\${timestamp}.\${rawBody}\`).digest('hex')
  const a = Buffer.from(signature)
  const b = Buffer.from(expected)
  return a.length === b.length && crypto.timingSafeEqual(a, b)
}`

export const PYTHON_VERIFY = `import hashlib, hmac, time

def verify_crewstudio(raw_body: bytes, headers, secret: str) -> bool:
    timestamp = headers.get("X-CrewStudio-Timestamp", "")
    signature = headers.get("X-CrewStudio-Signature", "")
    if not timestamp.isdigit() or abs(time.time() - int(timestamp)) > 300:
        return False  # older than 5 minutes
    mac = hmac.new(secret.encode(), f"{timestamp}.".encode() + raw_body, hashlib.sha256)
    return hmac.compare_digest(signature, "sha256=" + mac.hexdigest())`
