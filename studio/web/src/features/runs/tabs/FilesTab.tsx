import { useEffect, useRef } from 'react'
import { FolderOpen, RefreshCw } from 'lucide-react'
import { runsApi } from '../../../api/endpoints'
import { Button } from '../../../components/Button'
import { Panel } from '../../../components/PageHeader'
import { EmptyState, ErrorState, Loading } from '../../../components/States'
import { useAsync } from '../../../lib/useAsync'
import { formatBytes } from '../../workorders/files/attachments'
import { FileList, type FileItem } from '../../workorders/files/FileList'

type FilesTabProps = {
  /** The work order's input files */
  inputs: readonly FileItem[]
  runId: string | null
  live: boolean
}

const total = (files: readonly FileItem[]) => formatBytes(files.reduce((a, f) => a + f.size_bytes, 0))

/** Input files, files the agents wrote, and the full transcript + console log of the run. */
export function FilesTab({ inputs, runId, live }: FilesTabProps) {
  const res = useAsync((signal) => (runId ? runsApi.artifacts(runId, signal) : Promise.resolve([])), [runId])
  const { reload } = res
  // Files are stored after the run finishes, so fetch again when a live run ends (the first load covers the rest)
  const wasLive = useRef(live)
  useEffect(() => {
    if (wasLive.current && !live) reload()
    wasLive.current = live
  }, [live, reload])

  const artifacts = (res.data ?? []).filter((a) => a.kind === 'artifact').sort((a, b) => a.filename.localeCompare(b.filename))
  const transcripts = (res.data ?? []).filter((a) => a.kind === 'transcript')

  return (
    <div className="flex flex-col gap-4">
      <Panel title={`Input files${inputs.length ? ` · ${inputs.length} · ${total(inputs)}` : ''}`} bodyClassName="p-3">
        {inputs.length === 0 ? <p className="text-[12px] text-faint">No input files.</p> : <FileList files={inputs} />}
      </Panel>
      {runId && (
        <>
          <Panel
            title={`Files the agents wrote${artifacts.length ? ` · ${artifacts.length} · ${total(artifacts)}` : ''}`}
            actions={<Button size="sm" variant="ghost" icon={<RefreshCw className={`size-3.5 ${res.loading ? 'animate-spin' : ''}`} />} onClick={reload}>Refresh</Button>}
            bodyClassName="p-3"
          >
            {res.error && !res.data ? (
              <ErrorState error={res.error} onRetry={reload} title="Could not load the run's files" />
            ) : !res.data ? (
              <Loading label="Loading files…" />
            ) : artifacts.length === 0 ? (
              <EmptyState icon={<FolderOpen className="size-6" />} title={live ? 'Files are saved when the run finishes' : 'The agents wrote no files'} hint="Anything written to the run's workspace is stored here." />
            ) : (
              <FileList files={artifacts} />
            )}
          </Panel>
          <Panel title="Transcript and console log" bodyClassName="p-3">
            {transcripts.length === 0 ? (
              <p className="text-[12px] text-faint">{live ? 'Saved when the run finishes.' : res.data ? 'No transcript was stored for this run.' : 'Loading…'}</p>
            ) : (
              <>
                <p className="mb-2 text-[12px] text-muted">The untruncated record of every prompt, response, tool call and console line.</p>
                <FileList files={transcripts} gallery={false} />
              </>
            )}
          </Panel>
        </>
      )}
    </div>
  )
}
