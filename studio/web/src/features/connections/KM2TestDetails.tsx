import { FolderOpen, TriangleAlert } from 'lucide-react'
import type { ConnectionTestResult } from '../../api/types.crewstate'
import { Chip } from '../../components/StatusBadge'

/** The folders a KM2 connection's own settings let crews save to: the default first, then the listed ones. */
export function saveFolders(config: Record<string, string>): { defaultId: string; listed: string[] } {
  const defaultId = (config.default_folder_id ?? '').toLowerCase()
  const listed = (config.write_folder_ids ?? '').split(',').map((id) => id.trim().toLowerCase()).filter((id) => id && id !== defaultId)
  return { defaultId, listed }
}

/** What a KM2 connection test found: the folders its key can see (marking where crews may save) and any warnings. */
export function KM2TestDetails({ result, config }: { result: ConnectionTestResult; config: Record<string, string> }) {
  const folders = result.folders ?? []
  const warnings = result.warnings ?? []
  const { defaultId, listed } = saveFolders(config)
  const more = (result.folder_count ?? folders.length) - folders.length
  return (
    <div className="mt-1.5 flex flex-col gap-1.5 text-[12px]" aria-label="KM2 test details">
      {warnings.map((w) => (
        <div key={w} className="flex items-start gap-1.5 text-warn"><TriangleAlert className="mt-0.5 size-3.5 shrink-0" />{w}</div>
      ))}
      {folders.length > 0 && (
        <details>
          <summary className="cursor-pointer text-muted">Folders the key can see ({result.folder_count ?? folders.length})</summary>
          <ul className="mt-1 flex max-h-56 flex-col gap-0.5 overflow-y-auto">
            {folders.map((f) => (
              <li key={f.id} className="flex flex-wrap items-center gap-1.5">
                <FolderOpen className="size-3.5 text-faint" />
                <span>{f.path || f.name}</span>
                <span className="font-mono text-[11px] text-faint">{f.id}</span>
                {f.id === defaultId && <Chip tone="ok">default for saves</Chip>}
                {listed.includes(f.id) && <Chip tone="neutral">crews may save</Chip>}
              </li>
            ))}
            {more > 0 && <li className="text-faint">and {more} more</li>}
          </ul>
        </details>
      )}
    </div>
  )
}
