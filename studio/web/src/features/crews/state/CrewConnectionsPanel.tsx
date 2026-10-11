import { useState } from 'react'
import { Link } from 'react-router'
import { Pencil, PlugZap } from 'lucide-react'
import { connectionsApi, crewStateApi } from '../../../api/endpoints.crewstate'
import type { Connection } from '../../../api/types.crewstate'
import { Button } from '../../../components/Button'
import { MultiSelect } from '../../../components/MultiSelect'
import { Panel } from '../../../components/PageHeader'
import { AsyncView } from '../../../components/States'
import { Chip } from '../../../components/StatusBadge'
import { notifyError, notifySuccess } from '../../../lib/notify'
import { useAsync } from '../../../lib/useAsync'

function Editor({ crewId, linked, onDone, onCancel }: { crewId: string; linked: Connection[]; onDone: (c: Connection[]) => void; onCancel: () => void }) {
  const all = useAsync((signal) => connectionsApi.list(signal), [])
  const [value, setValue] = useState(linked.map((c) => c.id))
  const [saving, setSaving] = useState(false)
  const save = async () => {
    setSaving(true)
    try {
      onDone(await crewStateApi.setConnections(crewId, value))
      notifySuccess('Saved the crew’s connections')
    } catch (err) {
      notifyError('Could not save the crew’s connections', err)
    } finally {
      setSaving(false)
    }
  }
  return (
    <AsyncView {...all}>
      {(list) => (
        <div className="flex flex-col gap-3">
          <MultiSelect
            label="Connections this crew's tools may use"
            options={[...list].sort((a, b) => a.name.localeCompare(b.name)).map((c) => ({ value: c.id, label: c.name, hint: c.kind }))}
            value={value}
            onChange={setValue}
            emptyText="No connections in your areas yet."
          />
          <div className="flex justify-end gap-2">
            <Button size="sm" variant="ghost" onClick={onCancel}>Cancel</Button>
            <Button size="sm" variant="primary" busy={saving} onClick={() => void save()}>Save connections</Button>
          </div>
        </div>
      )}
    </AsyncView>
  )
}

/**
 * Which connections the crew's tools may use. A run only gets the ones that are enabled and share an area with
 * it. Changing the list needs crews.edit; connections outside your areas stay linked and are not shown.
 */
export function CrewConnectionsPanel({ crewId, canEdit }: { crewId: string; canEdit: boolean }) {
  const res = useAsync((signal) => crewStateApi.connections(crewId, signal), [crewId])
  const [editing, setEditing] = useState(false)
  return (
    <Panel title="Connections" actions={canEdit && !editing && <Button size="sm" variant="ghost" icon={<Pencil className="size-3.5" />} onClick={() => setEditing(true)}>Change</Button>}>
      <AsyncView {...res}>
        {(linked) =>
          editing ? (
            <Editor crewId={crewId} linked={linked} onCancel={() => setEditing(false)} onDone={(next) => { res.setData(() => next); setEditing(false) }} />
          ) : linked.length === 0 ? (
            <p className="text-[12.5px] text-muted">This crew may not use any <Link className="text-accent" to="/connections">connections</Link> yet.</p>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {linked.map((c) => (
                <Chip key={c.id} tone={c.enabled ? 'blue' : 'neutral'} title={c.enabled ? c.kind : `${c.kind} (disabled)`}>
                  <PlugZap className="size-3" /> {c.name}{!c.enabled && ' (disabled)'}
                </Chip>
              ))}
            </div>
          )
        }
      </AsyncView>
    </Panel>
  )
}
