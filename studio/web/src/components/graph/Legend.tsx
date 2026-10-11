import type { EdgeLook } from './edgeStyle'

export function Legend({ items }: { items: { key: string; label: string; look: EdgeLook }[] }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-[11px] text-faint">
      {items.map((i) => (
        <span key={i.key} className="inline-flex items-center gap-1.5">
          <svg width="22" height="6" aria-hidden="true">
            <line x1="0" y1="3" x2="22" y2="3" stroke={i.look.color} strokeWidth={Math.max(1.5, i.look.width)} strokeDasharray={i.look.dash} />
          </svg>
          {i.label}
        </span>
      ))}
    </div>
  )
}
