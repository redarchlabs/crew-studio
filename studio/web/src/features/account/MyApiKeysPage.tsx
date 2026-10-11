import { Link } from 'react-router'
import { Info } from 'lucide-react'
import { PageHeader, Panel } from '../../components/PageHeader'
import { useAuth } from '../../lib/auth'
import { ApiKeysPanel, myKeySource } from '../admin/ApiKeysPanel'

/** Why the signed-in person cannot create keys right now, or null when they can. */
export function cannotCreateReason(canCreate: boolean, via: string | undefined): string | null {
  if (via === 'api_key') return 'You are using an API key. Sign in to Crew Studio to create keys; a key cannot create more keys.'
  if (!canCreate) return 'You do not have the "Create personal API keys" permission, so you cannot create keys and any you made earlier do not work. Ask an administrator if you need one.'
  return null
}

/** /account/api-keys: the signed-in person's own API keys. */
export function MyApiKeysPage() {
  const { me, can } = useAuth()
  const reason = cannotCreateReason(can('account.api_keys'), me?.via)
  const origin = window.location.origin

  return (
    <>
      <PageHeader
        eyebrow="Developers"
        title="My API keys"
        description="Keys for scripts and MCP clients such as Claude Code that call Crew Studio as you, with your permissions and functional areas or only some of them. Each key is shown once when you create it; Crew Studio stores only a one-way hash, so nobody can read it back."
      />
      <div className="flex flex-col gap-4">
        {reason && (
          <div className="flex items-start gap-2 rounded-md border border-line bg-panel px-3 py-2 text-[12.5px] text-muted" role="note">
            <Info className="mt-0.5 size-4 shrink-0 text-faint" />
            <span>{reason}</span>
          </div>
        )}
        <ApiKeysPanel
          source={myKeySource(!reason, me ?? { permissions: [], all_areas: false, area_ids: [] })}
          intro="A key can do what you can, in your areas, unless you limit it when you create it: to the REST API or MCP, and to some of your permissions and areas. Revoke one as soon as you stop using it or think it may have leaked."
          emptyText={reason ? 'You have no API keys.' : 'No API keys yet. Create one for the script or tool that will call Crew Studio.'}
        />
        <Panel title="Using a key">
          <p className="mb-2 text-[12.5px] text-muted">Send the key in the Authorization header:</p>
          <pre className="overflow-x-auto rounded-md border border-line bg-raised p-2 font-mono text-[12px]">{`curl -H "Authorization: Bearer csk_…" ${origin}/api/auth/me`}</pre>
          <p className="mt-2 text-[12.5px] text-muted">
            Try requests in the <Link to="/api-docs" className="text-accent">API explorer</Link>.
          </p>
        </Panel>
      </div>
    </>
  )
}
