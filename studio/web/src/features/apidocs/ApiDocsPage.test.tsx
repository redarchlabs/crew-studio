import { render, screen } from '@testing-library/react'
import type { ReactNode } from 'react'
import { describe, expect, it, vi } from 'vitest'

type SwaggerProps = {
  url?: string
  requestInterceptor?: (req: unknown) => unknown
  persistAuthorization?: boolean
  deepLinking?: boolean
  docExpansion?: string
  defaultModelsExpandDepth?: number
  displayRequestDuration?: boolean
  tryItOutEnabled?: boolean
}
const captured: { props?: SwaggerProps } = {}

vi.mock('swagger-ui-react', () => ({
  default: (props: SwaggerProps): ReactNode => {
    captured.props = props
    return <div data-testid="swagger" data-url={props.url} />
  },
}))
vi.mock('swagger-ui-react/swagger-ui.css', () => ({}))

import ApiDocsPage from './ApiDocsPage'
import { autoCleanup } from '../../lib/testUtils'

autoCleanup()

describe('ApiDocsPage', () => {
  it('renders the header, the safety notes and Swagger UI', () => {
    render(<ApiDocsPage />)
    expect(screen.getByRole('heading', { level: 1, name: 'API explorer' })).toBeTruthy()
    expect(screen.getByText('Developers')).toBeTruthy()
    expect(screen.getByText(/real calls/i)).toBeTruthy()
    expect(screen.getByText(/as you/)).toBeTruthy()
    expect(screen.getByText('csk_…').tagName).toBe('CODE')
    expect(screen.getByTestId('swagger')).toBeTruthy()
  })

  it('points Swagger UI at the served OpenAPI document', () => {
    render(<ApiDocsPage />)
    expect(screen.getByTestId('swagger').getAttribute('data-url')).toBe('/api/openapi.yaml')
  })

  it('configures Swagger UI safely: no persisted credentials, try-it off until authorised', () => {
    render(<ApiDocsPage />)
    expect(captured.props).toMatchObject({
      persistAuthorization: false,
      deepLinking: true,
      docExpansion: 'list',
      defaultModelsExpandDepth: -1,
      displayRequestDuration: true,
      tryItOutEnabled: false,
    })
  })

  it('routes outgoing requests through prepareRequest (cookies + CSRF header on multipart)', () => {
    render(<ApiDocsPage />)
    const out = captured.props!.requestInterceptor!({
      url: '/api/work-orders/w1/attachments',
      method: 'POST',
      headers: { 'Content-Type': 'multipart/form-data; boundary=x' },
    }) as { headers: Record<string, string>; credentials: string }
    expect(out.credentials).toBe('same-origin')
    expect(out.headers['X-Requested-With']).toBe('crewstudio')
  })

  it('leaves JSON requests without the multipart header', () => {
    render(<ApiDocsPage />)
    const out = captured.props!.requestInterceptor!({ url: '/api/runs', headers: { Accept: 'application/json' } }) as { headers: Record<string, string> }
    expect(out.headers['X-Requested-With']).toBeUndefined()
  })
})
