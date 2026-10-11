// swagger-ui-react ships no TypeScript types; these cover the props the API explorer uses.
declare module 'swagger-ui-react' {
  import type { ComponentType } from 'react'

  export type SwaggerUIProps = {
    url?: string
    requestInterceptor?: <T>(req: T) => T
    persistAuthorization?: boolean
    deepLinking?: boolean
    docExpansion?: 'list' | 'full' | 'none'
    defaultModelsExpandDepth?: number
    displayRequestDuration?: boolean
    tryItOutEnabled?: boolean
  }

  const SwaggerUI: ComponentType<SwaggerUIProps>
  export default SwaggerUI
}
