import { fireEvent, render, screen } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { ImageLightbox, type LightboxImage } from './ImageLightbox'
import { autoCleanup } from '../lib/testUtils'

autoCleanup()
const imgs: LightboxImage[] = [
  { src: '/a.png', title: 'first.png', downloadUrl: '/dl/a' },
  { src: '/b.png', title: 'second.png' },
  { src: '/c.png', title: 'third.png' },
]

function setup(index = 0, images = imgs) {
  const onIndex = vi.fn()
  const onClose = vi.fn()
  const utils = render(<ImageLightbox images={images} index={index} onIndex={onIndex} onClose={onClose} />)
  return { onIndex, onClose, ...utils }
}

describe('ImageLightbox', () => {
  it('shows the current image in a labelled modal dialog with its title', () => {
    setup(1)
    expect(screen.getByRole('dialog', { name: 'second.png' }).getAttribute('aria-modal')).toBe('true')
    const img = screen.getByRole('img', { name: 'second.png' }) as HTMLImageElement
    expect(img.getAttribute('src')).toBe('/b.png')
    expect(screen.getByText('2 / 3')).toBeTruthy()
  })

  it('renders nothing when the index is out of range or there are no images', () => {
    const a = setup(5)
    expect(a.container.firstChild).toBeNull()
    a.unmount()
    const b = setup(0, [])
    expect(b.container.firstChild).toBeNull()
  })

  it('a single image has no counter or arrows', () => {
    setup(0, [imgs[1]])
    expect(screen.queryByText(/\//)).toBeNull()
    expect(screen.queryByRole('button', { name: 'Next image' })).toBeNull()
    expect(screen.queryByRole('button', { name: 'Previous image' })).toBeNull()
  })

  it('offers a download link only when a downloadUrl is given', () => {
    const { unmount } = setup(0)
    const a = screen.getByRole('link', { name: 'Download' })
    expect(a.getAttribute('href')).toBe('/dl/a')
    expect(a.hasAttribute('download')).toBe(true)
    unmount()
    setup(1)
    expect(screen.queryByRole('link', { name: 'Download' })).toBeNull()
  })

  it('next/previous buttons wrap around', () => {
    const { onIndex } = setup(2)
    fireEvent.click(screen.getByRole('button', { name: 'Next image' }))
    expect(onIndex).toHaveBeenLastCalledWith(0)
    fireEvent.click(screen.getByRole('button', { name: 'Previous image' }))
    expect(onIndex).toHaveBeenLastCalledWith(1)
  })

  it('previous from the first wraps to the last', () => {
    const { onIndex } = setup(0)
    fireEvent.click(screen.getByRole('button', { name: 'Previous image' }))
    expect(onIndex).toHaveBeenCalledWith(2)
  })

  it('arrow keys navigate (wrapping) and Escape closes', () => {
    const { onIndex, onClose } = setup(2)
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    expect(onIndex).toHaveBeenLastCalledWith(0)
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(onIndex).toHaveBeenLastCalledWith(1)
    fireEvent.keyDown(document, { key: 'x' })
    expect(onIndex).toHaveBeenCalledTimes(2)
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).toHaveBeenCalledTimes(1)
  })

  it('arrow keys do nothing for a single image', () => {
    const { onIndex } = setup(0, [imgs[0]])
    fireEvent.keyDown(document, { key: 'ArrowRight' })
    fireEvent.keyDown(document, { key: 'ArrowLeft' })
    expect(onIndex).not.toHaveBeenCalled()
  })

  it('closes from the Close button and by pressing the backdrop, not the image', () => {
    const { onClose } = setup(0)
    fireEvent.click(screen.getByRole('button', { name: 'Close' }))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.mouseDown(screen.getByRole('img'))
    expect(onClose).toHaveBeenCalledTimes(1)
    fireEvent.mouseDown(screen.getByRole('dialog'))
    expect(onClose).toHaveBeenCalledTimes(2)
    fireEvent.mouseDown(screen.getByRole('img').parentElement!)
    expect(onClose).toHaveBeenCalledTimes(3)
  })

  it('removes its key listener on unmount', () => {
    const { onClose, unmount } = setup(0)
    unmount()
    fireEvent.keyDown(document, { key: 'Escape' })
    expect(onClose).not.toHaveBeenCalled()
  })
})
