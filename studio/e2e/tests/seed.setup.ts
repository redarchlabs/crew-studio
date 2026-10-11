import { test as setup } from '@playwright/test'
import { ROLE_USERS, statePath, type RoleKey } from '../support/accounts.ts'
import { apiAs, findByName, unwrap } from '../support/api.ts'
import { writeSessionState } from '../support/sessions.ts'

setup('create the role accounts and their sessions', async () => {
  const admin = await apiAs('admin')
  for (const [key, spec] of Object.entries(ROLE_USERS)) {
    const role = await findByName<{ id: string; name: string }>(admin, '/api/admin/roles', spec.role)
    const areaIds: string[] = []
    for (const area of spec.areas) areaIds.push((await findByName<{ id: string; name: string }>(admin, '/api/areas', area)).id)
    const user = await unwrap<{ id: string }>(
      await admin.post('/api/admin/users', {
        data: { kind: 'human', email: spec.email, name: spec.name, status: 'active', all_areas: false, area_ids: areaIds, role_ids: [role.id], permissions: [] },
      }),
      201,
    )
    writeSessionState(statePath(key as RoleKey), user.id)
  }
  await admin.dispose()
})
