// Validation for the callback allowed-hosts list: bare host names or IP addresses only.

const IPV4_RE = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/
const LABEL_RE = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/

export function normalizeHost(raw: string): string {
  return raw.trim().toLowerCase().replace(/\.$/, '')
}

function isIpv4(host: string): boolean {
  return IPV4_RE.test(host)
}

function isIpv6(host: string): boolean {
  if (!/^[0-9a-f:]+$/.test(host) || !host.includes(':')) return false
  const doubles = host.split('::').length - 1
  if (doubles > 1) return false
  const groups = host.split(':').filter((g) => g !== '')
  if (groups.some((g) => g.length > 4)) return false
  return doubles === 1 ? groups.length < 8 : groups.length === 8
}

function isHostname(host: string): boolean {
  if (host.length > 253) return false
  const labels = host.split('.')
  // All-numeric dotted names are malformed IPv4 addresses, not host names
  if (labels.every((l) => /^\d+$/.test(l))) return false
  return labels.every((l) => LABEL_RE.test(l))
}

/** Why a host cannot be added, or undefined when it is fine. Expects a normalized host. */
export function hostProblem(host: string, existing: readonly string[]): string | undefined {
  if (!host) return 'Enter a host name or IP address'
  if (host.includes('://')) return 'Enter just the host, without http:// or https://'
  if (host.includes('/')) return 'Enter just the host, without a path'
  if (/\s/.test(host)) return 'Host names cannot contain spaces'
  if (host.startsWith('[')) return 'Enter IPv6 addresses without brackets'
  if (!isIpv6(host) && host.includes(':')) return 'Leave out the port; any port on an allowed host is accepted'
  if (!isIpv4(host) && !isIpv6(host) && !isHostname(host)) return 'Not a valid host name or IP address'
  if (existing.includes(host)) return 'Already on the list'
  return undefined
}
