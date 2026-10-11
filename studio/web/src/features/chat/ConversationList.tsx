import { NavLink } from 'react-router'
import type { Conversation } from '../../api/types.chat'
import { relative } from '../../lib/format'

/** The person's conversations with the chosen crew, most recent first. */
export function ConversationList({ items, now }: { items: readonly Conversation[]; now: number }) {
  if (items.length === 0) return <p className="px-1 text-[12px] text-faint">No conversations with this crew yet.</p>
  return (
    <nav aria-label="Conversations" className="flex flex-col gap-0.5">
      {items.map((c) => (
        <NavLink
          key={c.id}
          to={`/chat/${c.id}`}
          className={({ isActive }) => `rounded-md px-2 py-1.5 text-[12.5px] ${isActive ? 'bg-accent-soft text-accent' : 'hover:bg-panel-2'}`}
        >
          <span className="block truncate font-medium">{c.title || 'Untitled'}</span>
          <span className="block text-[11px] text-faint">{relative(c.updated_at, now)}</span>
        </NavLink>
      ))}
    </nav>
  )
}
