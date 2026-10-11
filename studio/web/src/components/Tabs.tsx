import type { ReactNode } from 'react'

export type TabDef<K extends string> = { key: K; label: ReactNode; badge?: ReactNode }

type TabsProps<K extends string> = { tabs: readonly TabDef<K>[]; active: K; onChange: (key: K) => void }

export function Tabs<K extends string>({ tabs, active, onChange }: TabsProps<K>) {
  return (
    <div role="tablist" className="flex gap-0.5 overflow-x-auto overflow-y-hidden border-b border-line">
      {tabs.map((t) => {
        const on = t.key === active
        return (
          <button
            key={t.key}
            role="tab"
            type="button"
            aria-selected={on}
            onClick={() => onChange(t.key)}
            className={`-mb-px flex items-center gap-1.5 whitespace-nowrap border-b-2 px-3 py-2 text-[13px] transition-colors ${on ? 'border-accent text-fg' : 'border-transparent text-muted hover:text-fg'}`}
          >
            {t.label}
            {t.badge !== undefined && <span className="rounded bg-panel-2 px-1 font-mono text-[10.5px] text-faint">{t.badge}</span>}
          </button>
        )
      })}
    </div>
  )
}
