import { Suspense, useEffect, useState } from 'react'
import { NavLink, Outlet, useLocation } from 'react-router'
import { LogOut, Menu, Moon, Sun, X } from 'lucide-react'
import { Toaster } from 'sonner'
import { useTheme } from '../lib/theme'
import { useAuth } from '../lib/auth'
import { Loading } from '../components/States'
import { NAV, type NavItem, type QuestionsBadge } from './nav'
import { useWaitingQuestions, WaitingQuestionsProvider } from '../features/human/waitingQuestions'

function Brand() {
  return (
    <div className="flex items-center gap-2.5 px-4 py-4">
      <svg viewBox="0 0 32 32" className="size-7" aria-hidden="true">
        <rect width="32" height="32" rx="7" fill="var(--panel-2)" stroke="var(--line-strong)" />
        <circle cx="9" cy="16" r="3.5" fill="var(--accent)" />
        <circle cx="23" cy="9" r="3.5" fill="var(--teal)" />
        <circle cx="23" cy="23" r="3.5" fill="var(--teal)" />
        <path d="M12 15l8-5M12 17l8 5" stroke="var(--fg-muted)" strokeWidth="1.6" />
      </svg>
      <div className="leading-tight">
        <div className="text-[14px] font-semibold tracking-tight">Crew Studio</div>
        <div className="font-mono text-[10px] text-faint">crewai · console</div>
      </div>
    </div>
  )
}

function SidebarNav({ onNavigate }: { onNavigate: () => void }) {
  const { can, canAny } = useAuth()
  const visible = (item: NavItem) => !item.perm || (Array.isArray(item.perm) ? canAny(...item.perm) : can(item.perm))
  const sections = NAV.map((section) => ({ ...section, items: section.items.filter(visible) })).filter((s) => s.items.length > 0)
  const questions = useWaitingQuestions()
  // A question from an agent chat's hand-off is answered in its conversation; the others on the work order
  const waitingOn = (badge: QuestionsBadge) => questions.filter((q) => Boolean(q.conversation_id) === (badge === 'chat')).length
  return (
    <nav className="flex-1 overflow-y-auto px-2 pb-4">
      {sections.map((section) => (
        <div key={section.title} className="mt-3">
          <div className="eyebrow px-2 pb-1">{section.title}</div>
          {section.items.map(({ to, label, icon: Icon, end, questionsBadge }) => {
            const waiting = questionsBadge ? waitingOn(questionsBadge) : 0
            return (
              <NavLink
                key={to}
                to={to}
                end={end}
                onClick={onNavigate}
                className={({ isActive }) =>
                  `group relative flex items-center gap-2.5 rounded-md px-2 py-1.5 text-[13px] transition-colors ${isActive ? 'bg-panel-2 text-fg' : 'text-muted hover:bg-panel-2/60 hover:text-fg'}`
                }
              >
                {({ isActive }) => (
                  <>
                    <span className={`absolute left-0 top-1.5 bottom-1.5 w-0.5 rounded-full ${isActive ? 'bg-accent' : 'bg-transparent'}`} />
                    <Icon className={`size-4 ${isActive ? 'text-accent' : ''}`} />
                    {label}
                    {waiting > 0 && (
                      <span className="ml-auto rounded-full bg-accent px-1.5 font-mono text-[10.5px] font-semibold text-accent-fg" title={`${waiting} question${waiting === 1 ? '' : 's'} waiting for you`}>
                        {waiting}
                      </span>
                    )}
                  </>
                )}
              </NavLink>
            )
          })}
        </div>
      ))}
    </nav>
  )
}

function UserMenu() {
  const { me, logout } = useAuth()
  if (!me) return null
  return (
    <div className="flex items-center gap-2 px-2 py-1.5">
      <div className="min-w-0 flex-1 leading-tight">
        <div className="truncate text-[12.5px] font-medium" title={me.name}>{me.name}</div>
        <div className="truncate text-[11px] text-faint" title={me.email}>{me.email}</div>
      </div>
      <button
        type="button"
        aria-label="Sign out"
        title="Sign out"
        className="rounded-md p-1.5 text-muted hover:bg-panel-2 hover:text-fg"
        onClick={() => void logout()}
      >
        <LogOut className="size-4" />
      </button>
    </div>
  )
}

export function AppShell() {
  const { me } = useAuth()
  const { theme, toggle } = useTheme()
  const [open, setOpen] = useState(false)
  const location = useLocation()
  useEffect(() => setOpen(false), [location.pathname])

  const themeButton = (
    <button type="button" onClick={toggle} className="flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-[12.5px] text-muted hover:bg-panel-2 hover:text-fg">
      {theme === 'dark' ? <Sun className="size-4" /> : <Moon className="size-4" />}
      {theme === 'dark' ? 'Light mode' : 'Dark mode'}
    </button>
  )

  return (
    <WaitingQuestionsProvider enabled={me?.via === 'session'}>
    <div className="flex h-full min-h-0">
      {open && <div className="fixed inset-0 z-30 bg-black/50 md:hidden" onClick={() => setOpen(false)} />}
      <aside
        className={`fixed inset-y-0 left-0 z-40 flex w-56 flex-col border-r border-line bg-raised transition-transform md:static md:translate-x-0 ${open ? 'translate-x-0' : '-translate-x-full'}`}
      >
        <div className="flex items-center justify-between">
          <Brand />
          <button type="button" aria-label="Close menu" className="mr-2 p-1 text-muted md:hidden" onClick={() => setOpen(false)}>
            <X className="size-4" />
          </button>
        </div>
        <SidebarNav onNavigate={() => setOpen(false)} />
        <div className="border-t border-line p-2">
          <UserMenu />
          {themeButton}
        </div>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <div className="flex items-center gap-2 border-b border-line bg-raised px-3 py-2 md:hidden">
          <button type="button" aria-label="Open menu" className="p-1 text-muted" onClick={() => setOpen(true)}>
            <Menu className="size-5" />
          </button>
          <span className="font-semibold">Crew Studio</span>
        </div>
        <main className="min-h-0 flex-1 overflow-y-auto">
          <div className="mx-auto w-full max-w-[1500px] px-4 py-5 md:px-6">
            <Suspense fallback={<Loading />}>
              <Outlet />
            </Suspense>
          </div>
        </main>
      </div>
      <Toaster theme={theme} position="bottom-right" richColors closeButton />
    </div>
    </WaitingQuestionsProvider>
  )
}
