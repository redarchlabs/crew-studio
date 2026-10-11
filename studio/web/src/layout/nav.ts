import {
  Bot,
  BookOpenText,
  CalendarClock,
  ClipboardList,
  Cpu,
  DollarSign,
  History,
  Key,
  KeyRound,
  KeySquare,
  LayoutDashboard,
  LifeBuoy,
  ListChecks,
  MessagesSquare,
  Play,
  PlugZap,
  ScrollText,
  Settings,
  Shapes,
  ShieldCheck,
  UserCog,
  Users,
  Webhook,
  Workflow,
  type LucideIcon,
} from 'lucide-react'
import type { Permission } from '../api/types'

export type NavItem = {
  to: string
  label: string
  icon: LucideIcon
  end?: boolean
  /** Shown only to people holding this permission (or any of these); omit to show to everyone signed in */
  perm?: Permission | Permission[]
  /** Shows how many questions are waiting on the signed-in person here: a crew's on Work orders, a hand-off's on Chat */
  questionsBadge?: QuestionsBadge
}
export type NavSection = { title: string; items: NavItem[] }
export type QuestionsBadge = 'work_orders' | 'chat'

export const NAV: NavSection[] = [
  {
    title: 'Console',
    items: [
      { to: '/', label: 'Overview', icon: LayoutDashboard, end: true, perm: 'overview.view' },
      { to: '/work-orders', label: 'Work orders', icon: ClipboardList, perm: 'work_orders.view', questionsBadge: 'work_orders' },
      { to: '/runs', label: 'Runs', icon: Play, perm: 'runs.view' },
      { to: '/chat', label: 'Chat', icon: MessagesSquare, perm: 'crews.chat', questionsBadge: 'chat' },
    ],
  },
  {
    title: 'Build',
    items: [
      { to: '/crews', label: 'Crews', icon: Workflow, perm: 'crews.view' },
      { to: '/agents', label: 'Agents', icon: Bot, perm: 'agents.view' },
      { to: '/tasks', label: 'Tasks', icon: ListChecks, perm: 'tasks.view' },
    ],
  },
  {
    title: 'Automate',
    items: [
      { to: '/schedules', label: 'Schedules', icon: CalendarClock, perm: 'schedules.view' },
      { to: '/triggers', label: 'Triggers', icon: Webhook, perm: 'triggers.view' },
    ],
  },
  {
    title: 'Configure',
    items: [
      { to: '/llms', label: 'LLMs', icon: Cpu, perm: 'llms.view' },
      { to: '/pricing', label: 'Pricing', icon: DollarSign, perm: 'pricing.view' },
      { to: '/settings', label: 'Settings', icon: Settings, perm: 'settings.view' },
      { to: '/connections', label: 'Connections', icon: PlugZap, perm: 'connections.view' },
      { to: '/history', label: 'History', icon: History, perm: ['history.view', 'admin.audit'] },
    ],
  },
  {
    title: 'Developers',
    items: [
      { to: '/api-docs', label: 'API explorer', icon: BookOpenText },
      { to: '/account/api-keys', label: 'My API keys', icon: KeySquare, perm: 'account.api_keys' },
    ],
  },
  {
    title: 'Admin',
    items: [
      { to: '/admin/users', label: 'People', icon: Users, perm: 'admin.users' },
      { to: '/admin/service-accounts', label: 'Service accounts', icon: KeyRound, perm: 'admin.service_accounts' },
      { to: '/admin/api-keys', label: 'API keys', icon: Key, perm: ['admin.users', 'admin.service_accounts'] },
      { to: '/admin/roles', label: 'Roles', icon: UserCog, perm: ['admin.roles', 'admin.users'] },
      { to: '/admin/areas', label: 'Functional areas', icon: Shapes, perm: 'admin.areas' },
      { to: '/admin/access', label: 'Sign-in settings', icon: ShieldCheck, perm: 'admin.access' },
      { to: '/admin/audit', label: 'Audit log', icon: ScrollText, perm: 'admin.audit' },
    ],
  },
  {
    title: 'Support',
    // Everyone signed in; the server filters help articles by permission
    items: [{ to: '/help', label: 'Help', icon: LifeBuoy }],
  },
]
