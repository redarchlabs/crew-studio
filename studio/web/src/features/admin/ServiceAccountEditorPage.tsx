import { AccountEditorRoute } from './AccountEditorRoute'

/** /admin/service-accounts/:id ("new" to create one). */
export function ServiceAccountEditorPage() {
  return <AccountEditorRoute kind="service" />
}
