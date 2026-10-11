import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen } from '@testing-library/react'
import { makeRun, makeWorkOrder, renderRoute } from '../testUtils'
import { makePreview } from '../../schedules/watch/testUtils'
import { BriefTab } from './BriefTab'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

afterEach(cleanup)

describe('a watched work order', () => {
  it('shows what the watch found instead of raw JSON inputs', () => {
    const p = makePreview()
    const inputs = { changes: JSON.stringify(p.changes), followups_due: JSON.stringify(p.followups_due), dry_run: 'false', topic: 'AI' }
    renderRoute(<BriefTab workOrder={makeWorkOrder({ inputs })} run={makeRun({ inputs })} journal={undefined} />)
    expect(screen.getByText('What the watch found')).toBeTruthy()
    expect(screen.getByText('PRJ-3')).toBeTruthy()
    expect(screen.getByText('topic')).toBeTruthy()
    expect(screen.queryByText('changes')).toBeNull()
    expect(screen.queryByText('dry run')).toBeNull()
  })

  it('keeps inputs that are not watch JSON as they are', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ inputs: { changes: 'my own text' } })} run={null} journal={undefined} />)
    expect(screen.queryByText('What the watch found')).toBeNull()
    expect(screen.getByText('changes')).toBeTruthy()
    expect(screen.getByText('my own text')).toBeTruthy()
  })
})
