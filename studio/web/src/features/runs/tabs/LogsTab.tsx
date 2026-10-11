import { useEffect, useRef, useState } from 'react'
import { Pause, Play, Terminal } from 'lucide-react'
import { Button } from '../../../components/Button'
import { EmptyState } from '../../../components/States'
import { CopyButton } from '../../../components/misc'
import type { LogLine } from '../timeline/timelineReducer'

/** Raw CrewAI console output; follows new lines unless paused. */
export function LogsTab({ logs }: { logs: readonly LogLine[] }) {
  const [paused, setPaused] = useState(false)
  const [frozenAt, setFrozenAt] = useState(logs.length)
  const [filter, setFilter] = useState('')
  const paneRef = useRef<HTMLDivElement>(null)
  const shown = paused ? logs.slice(0, frozenAt) : logs
  const q = filter.trim().toLowerCase()
  const visible = q ? shown.filter((l) => l.line.toLowerCase().includes(q)) : shown

  useEffect(() => {
    if (paused) return
    const el = paneRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [visible.length, paused])

  if (logs.length === 0) return <EmptyState icon={<Terminal className="size-6" />} title="No log output" hint="CrewAI console lines stream here while the run is active." />

  const togglePause = () => {
    if (!paused) setFrozenAt(logs.length)
    setPaused((p) => !p)
  }

  return (
    <div className="flex flex-col gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" icon={paused ? <Play className="size-3.5" /> : <Pause className="size-3.5" />} onClick={togglePause}>
          {paused ? `Resume${logs.length > frozenAt ? ` (${logs.length - frozenAt} new)` : ''}` : 'Pause'}
        </Button>
        <input className="field-input max-w-xs py-1 text-[12px]" placeholder="Filter lines…" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <span className="ml-auto font-mono text-[11px] text-faint">{visible.length}/{logs.length} lines</span>
        <CopyButton text={shown.map((l) => l.line).join('\n')} label="Copy all" />
      </div>
      <div ref={paneRef} className="h-[560px] overflow-auto rounded-lg border border-line bg-[#07090c] p-3 font-mono text-[11.5px] leading-relaxed text-[#c9d4df]">
        {visible.map((l) => (
          <div key={l.seq} className="whitespace-pre-wrap break-words">{l.line}</div>
        ))}
      </div>
    </div>
  )
}
