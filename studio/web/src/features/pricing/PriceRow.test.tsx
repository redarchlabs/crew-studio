import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { makePrice } from '../../lib/testFixtures'
import { EMPTY_PRICE, PriceEditRow, PriceViewRow, toPriceInput, validatePrice } from './PriceRow'

afterEach(cleanup)

const wrap = (row: React.ReactNode) => render(<table><tbody>{row}</tbody></table>)

describe('validatePrice', () => {
  const ok = { ...EMPTY_PRICE, model: 'm' }
  it('accepts zeros', () => expect(validatePrice(ok)).toEqual({}))
  it('requires a model (whitespace counts as empty)', () => {
    expect(validatePrice({ ...ok, model: '  ' })).toEqual({ model: 'Required' })
  })
  it.each(['input_per_mtok', 'cached_input_per_mtok', 'output_per_mtok'] as const)('rejects negative and NaN %s', (k) => {
    expect(validatePrice({ ...ok, [k]: -0.01 })).toEqual({ [k]: 'Must be ≥ 0' })
    expect(validatePrice({ ...ok, [k]: Number.NaN })).toEqual({ [k]: 'Must be ≥ 0' })
    expect(validatePrice({ ...ok, [k]: 12.5 })).toEqual({})
  })
  it('reports every problem at once', () => {
    expect(Object.keys(validatePrice({ model: '', input_per_mtok: -1, cached_input_per_mtok: -1, cache_write_per_mtok: -1, output_per_mtok: -1, notes: '' }))).toHaveLength(5)
  })
  it('allows a blank cache-write price (the input price) but not a negative one', () => {
    expect(validatePrice({ ...ok, cache_write_per_mtok: null })).toEqual({})
    expect(validatePrice({ ...ok, cache_write_per_mtok: 2.5 })).toEqual({})
    expect(validatePrice({ ...ok, cache_write_per_mtok: -1 })).toEqual({ cache_write_per_mtok: 'Must be ≥ 0, or blank' })
  })
})

describe('toPriceInput', () => {
  it('drops ids and timestamps', () => {
    const input = toPriceInput(makePrice({ id: 'p', model: 'm', notes: 'n' }))
    expect(input).toEqual({ model: 'm', input_per_mtok: 1, cached_input_per_mtok: 0.1, cache_write_per_mtok: null, output_per_mtok: 4, notes: 'n' })
  })
})

describe('PriceEditRow', () => {
  const setup = (over: Partial<Parameters<typeof PriceEditRow>[0]> = {}) => {
    const onSave = vi.fn()
    const onCancel = vi.fn()
    wrap(<PriceEditRow initial={{ model: 'm', input_per_mtok: 1, cached_input_per_mtok: 0.5, cache_write_per_mtok: null, output_per_mtok: 3, notes: 'n' }} errors={{}} saving={false} onSave={onSave} onCancel={onCancel} {...over} />)
    return { onSave, onCancel }
  }

  it('prefills the initial values', () => {
    setup()
    expect((screen.getByLabelText('Model') as HTMLInputElement).value).toBe('m')
    expect((screen.getByLabelText('Input per 1M tokens') as HTMLInputElement).value).toBe('1')
    expect((screen.getByLabelText('Cached input per 1M tokens') as HTMLInputElement).value).toBe('0.5')
    expect((screen.getByLabelText('Output per 1M tokens') as HTMLInputElement).value).toBe('3')
    expect((screen.getByLabelText('Notes') as HTMLInputElement).value).toBe('n')
  })

  it('saves edited values via the save button', () => {
    const { onSave } = setup()
    fireEvent.change(screen.getByLabelText('Model'), { target: { value: 'new-model' } })
    fireEvent.change(screen.getByLabelText('Input per 1M tokens'), { target: { value: '2.5' } })
    fireEvent.change(screen.getByLabelText('Cached input per 1M tokens'), { target: { value: '0.25' } })
    fireEvent.change(screen.getByLabelText('Output per 1M tokens'), { target: { value: '10' } })
    fireEvent.change(screen.getByLabelText('Notes'), { target: { value: 'https://x.y' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave).toHaveBeenCalledWith({ model: 'new-model', input_per_mtok: 2.5, cached_input_per_mtok: 0.25, cache_write_per_mtok: null, output_per_mtok: 10, notes: 'https://x.y' })
  })

  it('takes a cache-write price, and blank again means the input price', () => {
    const { onSave } = setup()
    const write = screen.getByLabelText('Cache write per 1M tokens') as HTMLInputElement
    expect(write.value).toBe('')
    expect(write.placeholder).toBe('1')
    fireEvent.change(write, { target: { value: '1.25' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[0][0].cache_write_per_mtok).toBe(1.25)
    fireEvent.change(write, { target: { value: '' } })
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(onSave.mock.calls[1][0].cache_write_per_mtok).toBeNull()
  })

  it('clearing a number yields NaN and shows an empty field', () => {
    const { onSave } = setup()
    fireEvent.change(screen.getByLabelText('Input per 1M tokens'), { target: { value: '' } })
    expect((screen.getByLabelText('Input per 1M tokens') as HTMLInputElement).value).toBe('')
    fireEvent.click(screen.getByRole('button', { name: 'Save' }))
    expect(Number.isNaN(onSave.mock.calls[0][0].input_per_mtok)).toBe(true)
  })

  it('Enter saves and Escape cancels', () => {
    const { onSave, onCancel } = setup()
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Enter' })
    expect(onSave).toHaveBeenCalledOnce()
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Escape' })
    expect(onCancel).toHaveBeenCalledOnce()
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'a' })
    expect(onSave).toHaveBeenCalledOnce()
  })

  it('cancel button cancels', () => {
    const { onCancel } = setup()
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(onCancel).toHaveBeenCalled()
  })

  it('Enter on the Cancel button does not save', () => {
    const { onSave } = setup()
    fireEvent.keyDown(screen.getByRole('button', { name: 'Cancel' }), { key: 'Enter' })
    expect(onSave).not.toHaveBeenCalled()
  })

  it('Enter in a field does not save again while a save is in flight', () => {
    const { onSave } = setup({ saving: true })
    fireEvent.keyDown(screen.getByLabelText('Model'), { key: 'Enter' })
    expect(onSave).not.toHaveBeenCalled()
  })

  it('marks invalid fields and exposes the message as a title', () => {
    setup({ errors: { model: 'Required', output_per_mtok: 'Must be ≥ 0' } })
    expect(screen.getByLabelText('Model').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Model').getAttribute('title')).toBe('Required')
    expect(screen.getByLabelText('Output per 1M tokens').getAttribute('aria-invalid')).toBe('true')
    expect(screen.getByLabelText('Input per 1M tokens').getAttribute('aria-invalid')).toBe('false')
  })

  it('disables save while saving', () => {
    setup({ saving: true })
    expect((screen.getByRole('button', { name: 'Save' }) as HTMLButtonElement).disabled).toBe(true)
  })
})

describe('PriceViewRow', () => {
  const price = makePrice({ id: 'p', model: 'gpt-x', input_per_mtok: 1.25, cached_input_per_mtok: 0.125, output_per_mtok: 10, notes: '' })

  it('formats prices in USD and shows a dash for empty notes', () => {
    wrap(<PriceViewRow price={price} usedBy={null} />)
    expect(screen.getByText('gpt-x')).toBeTruthy()
    expect(screen.getByText('$1.25')).toBeTruthy()
    expect(screen.getByText('$0.13')).toBeTruthy()
    expect(screen.getByText('$10.00')).toBeTruthy()
    expect(screen.getByText('—')).toBeTruthy()
    // No cache-write price: the input price applies
    expect(screen.getByText('input')).toBeTruthy()
  })

  it('shows a cache-write price when set', () => {
    wrap(<PriceViewRow price={{ ...price, cache_write_per_mtok: 1.5625 }} usedBy={null} />)
    expect(screen.getByText('$1.56')).toBeTruthy()
  })

  it('renders http(s) notes as a safe external link, other notes as text', () => {
    const { unmount } = wrap(<PriceViewRow price={{ ...price, notes: ' https://example.com/p ' }} usedBy={null} />)
    const a = screen.getByRole('link')
    expect(a.getAttribute('href')).toBe('https://example.com/p')
    expect(a.getAttribute('rel')).toBe('noopener noreferrer')
    expect(a.getAttribute('target')).toBe('_blank')
    unmount()
    wrap(<PriceViewRow price={{ ...price, notes: 'javascript:alert(1)' }} usedBy={null} />)
    expect(screen.queryByRole('link')).toBeNull()
    expect(screen.getByText('javascript:alert(1)')).toBeTruthy()
  })

  it('does not link notes with spaces after the url', () => {
    wrap(<PriceViewRow price={{ ...price, notes: 'see https://a.b now' }} usedBy={null} />)
    expect(screen.queryByRole('link')).toBeNull()
  })

  it('shows used-by counts with singular, plural, unused, and hides it when null', () => {
    const { unmount } = wrap(<PriceViewRow price={price} usedBy={['A']} />)
    expect(screen.getByText('1 LLM').getAttribute('title')).toBe('A')
    unmount()
    const u2 = wrap(<PriceViewRow price={price} usedBy={['A', 'B']} />)
    expect(screen.getByText('2 LLMs').getAttribute('title')).toBe('A, B')
    u2.unmount()
    const u3 = wrap(<PriceViewRow price={price} usedBy={[]} />)
    expect(screen.getByText('unused')).toBeTruthy()
    u3.unmount()
    wrap(<PriceViewRow price={price} usedBy={null} />)
    expect(screen.queryByText('unused')).toBeNull()
  })

  it('shows edit and delete only when provided; double click edits', () => {
    const onEdit = vi.fn()
    const onDelete = vi.fn()
    const { unmount } = wrap(<PriceViewRow price={price} usedBy={null} />)
    expect(screen.queryByRole('button')).toBeNull()
    unmount()
    wrap(<PriceViewRow price={price} usedBy={null} onEdit={onEdit} onDelete={onDelete} />)
    fireEvent.click(screen.getByRole('button', { name: 'Edit gpt-x' }))
    expect(onEdit).toHaveBeenCalledTimes(1)
    fireEvent.doubleClick(screen.getByText('gpt-x'))
    expect(onEdit).toHaveBeenCalledTimes(2)
    fireEvent.click(screen.getByRole('button', { name: 'Delete' }))
    expect(onDelete).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Confirm?' }))
    expect(onDelete).toHaveBeenCalledOnce()
  })
})
