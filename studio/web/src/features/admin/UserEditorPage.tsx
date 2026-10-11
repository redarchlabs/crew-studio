import { AccountEditorRoute } from './AccountEditorRoute'

/** /admin/users/:id ("new" to invite someone). */
export function UserEditorPage() {
  return <AccountEditorRoute kind="human" />
}
