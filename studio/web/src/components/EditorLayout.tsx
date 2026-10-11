import type { ReactNode } from 'react'

/** Form on the left, context panels on the right; stacks on narrow screens. */
export function EditorLayout({ main, side }: { main: ReactNode; side: ReactNode }) {
  return (
    <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_320px]">
      <div className="min-w-0">{main}</div>
      <aside className="flex min-w-0 flex-col gap-4">{side}</aside>
    </div>
  )
}

export function UsageList({ title, items, empty }: { title: string; items: { key: string; node: ReactNode }[]; empty: string }) {
  return (
    <div>
      <div className="eyebrow mb-1.5">{title}</div>
      {items.length === 0 ? (
        <div className="text-[12px] text-faint">{empty}</div>
      ) : (
        <ul className="flex flex-col gap-1">
          {items.map((i) => (
            <li key={i.key} className="truncate text-[12.5px]">
              {i.node}
            </li>
          ))}
        </ul>
      )}
    </div>
  )
}
