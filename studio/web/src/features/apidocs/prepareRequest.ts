// Adjusts requests Swagger UI sends so they pass the API's cross-site protections (docs/api.md, Envelope).

export type SwaggerRequest = {
  url: string
  method?: string
  headers: Record<string, string>
  body?: unknown
  credentials?: RequestCredentials
}

/** Multipart uploads need X-Requested-With; the session cookie is sent because the page is same-origin. */
export function prepareRequest<T extends SwaggerRequest>(req: T): T {
  const headers = { ...req.headers }
  const contentType = Object.entries(headers).find(([k]) => k.toLowerCase() === 'content-type')?.[1] ?? ''
  if (req.body instanceof FormData || contentType.startsWith('multipart/form-data')) headers['X-Requested-With'] = 'crewstudio'
  return { ...req, headers, credentials: 'same-origin' }
}
