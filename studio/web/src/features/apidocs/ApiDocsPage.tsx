import SwaggerUI from 'swagger-ui-react'
import 'swagger-ui-react/swagger-ui.css'
import { KeyRound, TriangleAlert } from 'lucide-react'
import { PageHeader } from '../../components/PageHeader'
import { prepareRequest, type SwaggerRequest } from './prepareRequest'

/**
 * Interactive API explorer (Swagger UI) over /api/openapi.yaml. "Try it out" calls the real API: as the
 * signed-in person by default, or as a service account after pasting its key under "Authorize".
 */
export default function ApiDocsPage() {
  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        eyebrow="Developers"
        title="API explorer"
        description="Every endpoint other systems can call, with live requests. The full contract is in studio/docs/api.md."
      />
      <div className="grid gap-3 md:grid-cols-2">
        <div className="flex gap-2.5 rounded-lg border border-line bg-raised p-3 text-[12.5px] text-muted">
          <KeyRound className="mt-0.5 size-4 shrink-0 text-accent" />
          <p>
            Requests run <strong className="text-fg">as you</strong>, with your permissions and functional areas. To test as an
            integration, choose <strong className="text-fg">Authorize</strong> and paste a service account key
            (<code className="font-mono">csk_…</code>) or, for <code className="font-mono">/hooks</code>, a trigger token. Keys are
            kept only for this page visit.
          </p>
        </div>
        <div className="flex gap-2.5 rounded-lg border border-warn/40 bg-warn-soft p-3 text-[12.5px] text-warn">
          <TriangleAlert className="mt-0.5 size-4 shrink-0" />
          <p>These are real calls. Starting a run or submitting a work order runs agents and spends money against your budget.</p>
        </div>
      </div>
      {/* Swagger UI ships its own light theme; keep it on a light panel in both app themes */}
      <div className="swagger-panel overflow-x-auto rounded-lg border border-line bg-white text-black">
        <SwaggerUI
          url="/api/openapi.yaml"
          requestInterceptor={(req) => prepareRequest(req as SwaggerRequest) as typeof req}
          persistAuthorization={false}
          deepLinking
          docExpansion="list"
          defaultModelsExpandDepth={-1}
          displayRequestDuration
          tryItOutEnabled={false}
        />
      </div>
    </div>
  )
}
