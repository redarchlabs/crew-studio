import { memo } from 'react'
import { Handle, Position, type Node, type NodeProps } from '@xyflow/react'
import { Bot, Cpu, ListChecks, TriangleAlert, Workflow, Wrench, type LucideIcon } from 'lucide-react'

export type EntityKind = 'crew' | 'task' | 'agent' | 'tool' | 'llm' | 'model'
export type EntityStatus = 'pending' | 'running' | 'done' | 'failed'

export type EntityNodeData = {
  kind: EntityKind
  label: string
  sublabel?: string
  status?: EntityStatus
  warnings?: string[]
  stats?: string[]
  focused?: boolean
  dimmed?: boolean
}

export type EntityFlowNode = Node<EntityNodeData, 'entity'>

const KIND: Record<EntityKind, { icon: LucideIcon; cls: string; name: string }> = {
  crew: { icon: Workflow, cls: 'text-accent bg-accent-soft', name: 'crew' },
  task: { icon: ListChecks, cls: 'text-blue bg-blue-soft', name: 'task' },
  agent: { icon: Bot, cls: 'text-teal bg-teal-soft', name: 'agent' },
  tool: { icon: Wrench, cls: 'text-violet bg-violet-soft', name: 'tool' },
  llm: { icon: Cpu, cls: 'text-muted bg-panel-2', name: 'llm' },
  model: { icon: Cpu, cls: 'text-muted bg-panel-2', name: 'model' },
}

const STATUS: Record<EntityStatus, string> = {
  pending: 'border-line',
  running: 'border-accent shadow-[0_0_0_3px_var(--accent-soft)]',
  done: 'border-ok/70',
  failed: 'border-danger',
}

const STATUS_DOT: Record<EntityStatus, string> = {
  pending: 'bg-faint',
  running: 'bg-accent pulse-dot',
  done: 'bg-ok',
  failed: 'bg-danger',
}

function EntityNodeView({ data, selected }: NodeProps<EntityFlowNode>) {
  const kind = KIND[data.kind]
  const Icon = kind.icon
  const warn = (data.warnings?.length ?? 0) > 0
  const border = data.status ? STATUS[data.status] : warn ? 'border-warn/70' : 'border-line'
  const ring = selected || data.focused ? 'outline outline-2 outline-offset-2 outline-accent' : ''
  return (
    <div
      className={`w-[210px] cursor-pointer rounded-lg border bg-panel px-2.5 py-2 text-left transition-opacity ${border} ${ring} ${data.dimmed ? 'opacity-35' : ''}`}
      title={data.warnings?.join('\n')}
    >
      <Handle type="target" position={Position.Left} className="!size-1.5 !border-0 !bg-line-strong" />
      <div className="flex items-center gap-2">
        <span className={`flex size-6 shrink-0 items-center justify-center rounded ${kind.cls}`}>
          <Icon className="size-3.5" />
        </span>
        <div className="min-w-0 flex-1">
          <div className="truncate text-[12.5px] font-medium leading-tight text-fg">{data.label}</div>
          <div className="truncate font-mono text-[10.5px] leading-tight text-faint">{data.sublabel ?? kind.name}</div>
        </div>
        {data.status && <span className={`size-2 shrink-0 rounded-full ${STATUS_DOT[data.status]}`} title={data.status} />}
        {warn && <TriangleAlert className="size-3.5 shrink-0 text-warn" />}
      </div>
      {data.stats && data.stats.length > 0 && (
        <div className="mt-1.5 flex flex-wrap gap-x-2 border-t border-line pt-1 font-mono text-[10.5px] text-muted">
          {data.stats.map((s) => (
            <span key={s}>{s}</span>
          ))}
        </div>
      )}
      <Handle type="source" position={Position.Right} className="!size-1.5 !border-0 !bg-line-strong" />
    </div>
  )
}

export const EntityNode = memo(EntityNodeView)

export const nodeTypes = { entity: EntityNode }
