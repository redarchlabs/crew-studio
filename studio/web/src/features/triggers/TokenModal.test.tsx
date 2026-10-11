import { cleanup, fireEvent, screen, within } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { makeTrigger, renderRoute, setupApi } from '../crews/testUtils'
import { TokenModal } from './TokenModal'

vi.mock('../../api/endpoints')

beforeEach(() => setupApi())
afterEach(cleanup)

const show = (over = {}, onClose = vi.fn()) => {
  renderRoute(<TokenModal trigger={makeTrigger({ id: 'tr9', ...over })} token="cst_ab_SECRETTOKEN" onClose={onClose} />)
  return onClose
}

describe('TokenModal', () => {
  it('shows the token, the hook URL, a curl example and the sample payload', async () => {
    show({ input_map: { company: 'customer.name' } })
    const dialog = await screen.findByRole('dialog')
    expect(within(dialog).getByText('Trigger token')).toBeTruthy()
    const pres = Array.from(dialog.querySelectorAll('pre')).map((p) => p.textContent ?? '')
    expect(pres[0]).toBe('cst_ab_SECRETTOKEN')
    expect(pres[1]).toBe('http://localhost/api/hooks/tr9')
    expect(pres[2]).toContain("curl -X POST 'http://localhost/api/hooks/tr9'")
    expect(pres[2]).toContain('Authorization: Bearer cst_ab_SECRETTOKEN')
    expect(pres[2]).toMatch(/Idempotency-Key: event-[0-9a-f]{8}/)
    expect(pres[2]).toContain('"customer":{"name":"<company>"}')
    expect(JSON.parse(pres[3])).toEqual({ customer: { name: '<company>' } })
  })

  it('uses a generic sample when nothing is mapped', async () => {
    show()
    const dialog = await screen.findByRole('dialog')
    const pres = Array.from(dialog.querySelectorAll('pre')).map((p) => p.textContent ?? '')
    expect(JSON.parse(pres[3])).toEqual({ event: 'example', message: 'Anything JSON up to 1 MB' })
  })

  it('warns that the token is shown once', async () => {
    show()
    expect(await screen.findByText(/shown only once/)).toBeTruthy()
  })

  it('has copy buttons for every snippet', async () => {
    show()
    await screen.findByRole('dialog')
    expect(screen.getAllByRole('button', { name: 'Copy' })).toHaveLength(4)
  })

  it('closes from the acknowledge button, the X and Escape', async () => {
    const onClose = show()
    await screen.findByRole('dialog')
    fireEvent.click(screen.getByRole('button', { name: 'I have saved the token' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(2)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(3)
  })
})
