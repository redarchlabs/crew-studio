import { useState } from 'react'
import { RefreshCw } from 'lucide-react'
import { connectionsApi } from '../../api/endpoints.crewstate'
import type { Connection, ConnectionTool, ConnectionToolChange, ToolEffectChoice } from '../../api/types.crewstate'
import { Button } from '../../components/Button'
import { Modal } from '../../components/Modal'
import { AsyncView } from '../../components/States'
import { Chip } from '../../components/StatusBadge'
import { relative } from '../../lib/format'
import { notifyError, notifySuccess } from '../../lib/notify'
import { useAsync } from '../../lib/useAsync'

type Props = { connection: Connection; canManage: boolean; onClose: () => void }

type Setting = { effect: ToolEffectChoice; enabled: boolean }

const settingsOf = (tools: ConnectionTool[]): Record<string, Setting> =>
  Object.fromEntries(tools.map((t) => [t.name, { effect: t.effect, enabled: t.enabled }]))

/** The tools whose effect or enabled flag differ from what is saved. */
export function changedTools(tools: ConnectionTool[], draft: Record<string, Setting>): ConnectionToolChange[] {
  return tools.flatMap((t) => {
    const d = draft[t.name]
    return d && (d.effect !== t.effect || d.enabled !== t.enabled) ? [{ name: t.name, effect: d.effect, enabled: d.enabled }] : []
  })
}

/** Whether agent chat may use a tool: enabled and marked Reads. */
const offered = (s: Setting) => s.enabled && s.effect === 'reads'

function ToolRow({ tool, setting, canManage, onChange }: { tool: ConnectionTool; setting: Setting; canManage: boolean; onChange: (s: Setting) => void }) {
  return (
    <tr className="border-b border-line align-top last:border-0 [&>td]:px-2 [&>td]:py-1.5">
      <td className="font-mono text-[12px]">{tool.name}</td>
      {/* The server's own words: plain text, never markup */}
      <td className="max-w-md whitespace-pre-wrap break-words text-[12.5px] text-muted">{tool.description || <span className="text-faint">no description</span>}</td>
      <td>
        <select aria-label={`Effect of ${tool.name}`} className="field-input py-1 text-[12.5px]" value={setting.effect} disabled={!canManage}
          onChange={(e) => onChange({ ...setting, effect: e.target.value as ToolEffectChoice })}>
          <option value="reads">Reads</option>
          <option value="changes">Changes</option>
        </select>
      </td>
      <td>
        <input type="checkbox" aria-label={`Enable ${tool.name}`} className="size-4 accent-[var(--accent)]" checked={setting.enabled}
          disabled={!canManage} onChange={(e) => onChange({ ...setting, enabled: e.target.checked })} />
      </td>
      <td>{offered(setting) ? <Chip tone="ok">in chat</Chip> : <Chip tone="neutral">not in chat</Chip>}</td>
    </tr>
  )
}

/**
 * The tools of an MCP connection (an outside MCP server): what Discover tools found, and what each one does. Agents may
 * use a tool in chat only when it is enabled and marked Reads; Studio calls the server itself with the saved secret.
 */
export function MCPToolsModal({ connection, canManage, onClose }: Props) {
  const res = useAsync((signal) => connectionsApi.tools(connection.id, signal), [connection.id])
  const [draft, setDraft] = useState<Record<string, Setting> | null>(null)
  const [skipped, setSkipped] = useState<string[]>([])
  const [review, setReview] = useState<string[]>([])
  const [busy, setBusy] = useState<'discover' | 'save' | null>(null)
  const tools = res.data ?? []
  const current = draft ?? settingsOf(tools)
  const changes = changedTools(tools, current)

  const discover = async () => {
    setBusy('discover')
    try {
      const out = await connectionsApi.discoverTools(connection.id)
      res.setData(() => out.tools)
      setDraft(null)
      setSkipped(out.skipped)
      setReview(out.review)
      notifySuccess(`${connection.name} offers ${out.found} tools; ${out.tools.length} kept`)
    } catch (err) {
      notifyError(`Could not discover the tools of ${connection.name}`, err)
    } finally {
      setBusy(null)
    }
  }
  const save = async () => {
    setBusy('save')
    try {
      const saved = await connectionsApi.setTools(connection.id, changes)
      res.setData(() => saved)
      setDraft(null)
      notifySuccess(`Saved the tools of ${connection.name}`)
    } catch (err) {
      notifyError('Could not save the tools', err)
    } finally {
      setBusy(null)
    }
  }

  const footer = (
    <>
      {canManage && (
        <Button icon={<RefreshCw className="size-4" />} busy={busy === 'discover'} disabled={changes.length > 0}
          title={changes.length > 0 ? 'Save or undo your changes first' : undefined} onClick={() => void discover()}>Discover tools</Button>
      )}
      <Button variant="ghost" onClick={onClose}>Close</Button>
      {canManage && <Button variant="primary" busy={busy === 'save'} disabled={changes.length === 0} onClick={() => void save()}>Save</Button>}
    </>
  )
  return (
    <Modal title={`Tools of ${connection.name}`} onClose={onClose} width="max-w-4xl" footer={footer}>
      <p className="mb-3 text-[12.5px] text-muted">
        The tools this MCP server offers. Agents that list this connection under Connections it may read can use a tool in chat only when
        it is enabled and marked Reads; nothing marked Changes is ever offered. On the first discovery a tool starts as Reads when the
        server says it only reads: check each one, since that is the server&apos;s claim. Tools that are new or changed later start as
        Changes. Names and descriptions are the server&apos;s own words.
      </p>
      {review.length > 0 && (
        <p className="mb-3 text-[12px] text-warn" role="status">
          New or changed since the last discovery, so marked Changes until you check them: {review.join(', ')}.
        </p>
      )}
      {skipped.length > 0 && (
        <details className="mb-3 text-[12px] text-warn">
          <summary className="cursor-pointer">{skipped.length} tools were left out</summary>
          <ul className="mt-1 list-disc pl-5">{skipped.map((s) => <li key={s}>{s}</li>)}</ul>
        </details>
      )}
      <AsyncView {...res}>
        {(list) => list.length === 0 ? (
          <p className="text-[12.5px] text-faint">No tools yet.{canManage && ' Discover them from the server.'}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full min-w-[640px] text-left text-[13px]">
              <thead className="border-b border-line">
                <tr className="eyebrow [&>th]:px-2 [&>th]:py-1.5 [&>th]:font-semibold"><th>Tool</th><th>Description</th><th>Effect</th><th>Enabled</th><th>Agent chat</th></tr>
              </thead>
              <tbody>
                {list.map((t) => (
                  <ToolRow key={t.name} tool={t} setting={current[t.name] ?? { effect: t.effect, enabled: t.enabled }} canManage={canManage}
                    onChange={(next) => setDraft({ ...current, [t.name]: next })} />
                ))}
              </tbody>
            </table>
            <p className="mt-2 text-[11.5px] text-faint">Last discovered {relative(list.reduce((a, t) => (t.discovered_at > a ? t.discovered_at : a), ''))}.</p>
          </div>
        )}
      </AsyncView>
    </Modal>
  )
}
