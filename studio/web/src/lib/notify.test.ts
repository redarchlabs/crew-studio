import { beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('sonner', () => ({ toast: { error: vi.fn(), success: vi.fn() } }))

import { toast } from 'sonner'
import { ApiError } from '../api/client'
import { notifyError, notifySuccess } from './notify'

beforeEach(() => vi.clearAllMocks())

describe('notifyError', () => {
  it('uses the error message for plain errors', () => {
    notifyError('Save failed', new Error('disk full'))
    expect(toast.error).toHaveBeenCalledWith('Save failed', { description: 'disk full' })
  })
  it('uses a generic message for non-errors', () => {
    notifyError('Save failed', 42)
    expect(toast.error).toHaveBeenCalledWith('Save failed', { description: 'Something went wrong.' })
  })
  it('uses the ApiError message when there are no details', () => {
    notifyError('x', new ApiError(500, { code: 'c', message: 'server broke' }))
    expect(toast.error).toHaveBeenCalledWith('x', { description: 'server broke' })
  })
  it('summarises up to four 422 details joined by a dot', () => {
    notifyError('x', new ApiError(422, { code: 'c', message: 'm', details: ['a', 'b', 'c', 'd'] }))
    expect(toast.error).toHaveBeenCalledWith('x', { description: 'a · b · c · d' })
  })
  it('truncates beyond four details with an ellipsis', () => {
    notifyError('x', new ApiError(422, { code: 'c', message: 'm', details: ['a', 'b', 'c', 'd', 'e'] }))
    expect(toast.error).toHaveBeenCalledWith('x', { description: 'a · b · c · d …' })
  })
})

describe('notifySuccess', () => {
  it('shows a success toast', () => {
    notifySuccess('Saved')
    expect(toast.success).toHaveBeenCalledWith('Saved')
  })
})
