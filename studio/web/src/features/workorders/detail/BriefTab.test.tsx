import { afterEach, describe, expect, it, vi } from 'vitest'
import { cleanup, screen, within } from '@testing-library/react'
import type { Run } from '../../../api/types'
import { makeAttachment, makeJournal, makeRun, makeWorkOrder, renderRoute } from '../testUtils'
import { BriefTab } from './BriefTab'

vi.mock('../../../api/endpoints', async () => (await import('../testUtils')).endpointsMock())

afterEach(cleanup)

const spec = (brief: string) => ({ spec: { work_order: { id: 'wo-1', number: 7, title: 't', brief, attachments: [] } } }) as unknown as Pick<Run, 'spec'>

describe('brief and inputs', () => {
  it('renders the work order brief as markdown', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ brief: '# Heading\n\nSome **bold**' })} run={null} journal={undefined} />)
    expect(screen.getByRole('heading', { name: 'Heading' })).toBeTruthy()
    expect(screen.getByText('bold').tagName).toBe('STRONG')
  })

  it('says when no brief was written', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ brief: '   ' })} run={null} journal={undefined} />)
    expect(screen.getByText('No brief was written.')).toBeTruthy()
  })

  it('prefers the brief frozen in the attempt and flags edits since', () => {
    const run = makeRun({ ...spec('Frozen brief') })
    renderRoute(<BriefTab workOrder={makeWorkOrder({ brief: 'New brief' })} run={run} journal={undefined} />)
    expect(screen.getByText('Frozen brief')).toBeTruthy()
    expect(screen.queryByText('New brief')).toBeNull()
    expect(screen.getByText('edited since this attempt')).toBeTruthy()
  })

  it('does not flag an unchanged brief', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ brief: 'Same' })} run={makeRun({ ...spec('Same') })} journal={undefined} />)
    expect(screen.queryByText('edited since this attempt')).toBeNull()
  })

  it('lists work order inputs, with a marker for empty values', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ inputs: { topic: 'AI', blank: '' } })} run={null} journal={undefined} />)
    expect(screen.getByText('topic')).toBeTruthy()
    expect(screen.getByText('AI')).toBeTruthy()
    expect(screen.getByText('""')).toBeTruthy()
    expect(screen.getByText('Inputs')).toBeTruthy()
  })

  it('shows the attempt inputs instead when a run is selected', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ inputs: { topic: 'new' } })} run={makeRun({ inputs: { topic: 'old' } })} journal={undefined} />)
    expect(screen.getByText('old')).toBeTruthy()
    expect(screen.queryByText('new')).toBeNull()
    expect(screen.getByText('Inputs (this attempt)')).toBeTruthy()
  })

  it('says when there are no inputs', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={null} journal={undefined} />)
    expect(screen.getByText('No inputs.')).toBeTruthy()
  })
})

describe('input files', () => {
  it('shows an empty message', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ attachments: [] })} run={null} journal={undefined} />)
    expect(screen.getByText('Input files (0)')).toBeTruthy()
    expect(screen.getByText('No input files.')).toBeTruthy()
  })

  it('treats missing attachments as none', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ attachments: undefined })} run={null} journal={undefined} />)
    expect(screen.getByText('No input files.')).toBeTruthy()
  })

  it('lists attached files with download links', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder({ attachments: [makeAttachment({ filename: 'spec.txt' })] })} run={null} journal={undefined} />)
    expect(screen.getByText('Input files (1)')).toBeTruthy()
    expect(screen.getByRole('link', { name: 'Download spec.txt' })).toBeTruthy()
  })
})

describe('results', () => {
  it('shows the run error', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={makeRun({ status: 'failed', error: 'Budget exceeded' })} journal={undefined} />)
    expect(screen.getByText('Budget exceeded')).toBeTruthy()
  })

  it('shows the final output with a copy button', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={makeRun({ output: 'The **answer**' })} journal={undefined} />)
    expect(screen.getByText('Final output')).toBeTruthy()
    expect(screen.getByText('answer').tagName).toBe('STRONG')
    expect(screen.getByRole('button', { name: /Copy/ })).toBeTruthy()
  })

  it('lists task outputs in position order, newest open when there is no final output', () => {
    const run = makeRun({
      output: null,
      task_outputs: [
        { position: 1, task_name: 'write', agent_role: 'Writer', raw: 'draft text' },
        { position: 0, task_name: 'research', agent_role: 'Analyst', raw: 'findings' },
      ],
    })
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={run} journal={undefined} />)
    const section = screen.getByText('Task outputs').parentElement!
    const titles = within(section).getAllByText(/^\d\. /).map((e) => e.textContent)
    expect(titles).toEqual(['1. research', '2. write'])
    expect(screen.getByText('Analyst')).toBeTruthy()
  })

  it('falls back to journal task outputs while the run is going, skipping tasks without output', () => {
    const journal = makeJournal({
      tasks: [
        { name: 'one', agent_role: 'A', status: 'done', started_at: null, finished_at: null, output: 'first result' },
        { name: 'two', agent_role: 'B', status: 'running', started_at: null, finished_at: null, output: null },
      ],
    })
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={makeRun({ status: 'running', task_outputs: [] })} journal={journal} />)
    expect(screen.getByText('1. one')).toBeTruthy()
    expect(screen.queryByText('2. two')).toBeNull()
    expect(screen.getByText('first result')).toBeTruthy()
  })

  it('shows nothing for results when there are none', () => {
    renderRoute(<BriefTab workOrder={makeWorkOrder()} run={null} journal={undefined} />)
    expect(screen.queryByText('Final output')).toBeNull()
    expect(screen.queryByText('Task outputs')).toBeNull()
  })
})
