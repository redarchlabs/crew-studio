import { useEffect } from 'react'
import { ChevronLeft, ChevronRight, Download, X } from 'lucide-react'
import { IconButton } from './Button'

export type LightboxImage = { src: string; title: string; downloadUrl?: string }

type ImageLightboxProps = {
  images: readonly LightboxImage[]
  index: number
  onIndex: (index: number) => void
  onClose: () => void
}

/** Full-screen image viewer: Escape closes, arrow keys move between images. */
export function ImageLightbox({ images, index, onIndex, onClose }: ImageLightboxProps) {
  const image = images[index]
  const count = images.length
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose()
      else if (e.key === 'ArrowRight' && count > 1) onIndex((index + 1) % count)
      else if (e.key === 'ArrowLeft' && count > 1) onIndex((index - 1 + count) % count)
    }
    document.addEventListener('keydown', onKey)
    return () => document.removeEventListener('keydown', onKey)
  }, [index, count, onIndex, onClose])

  if (!image) return null
  return (
    <div role="dialog" aria-modal="true" aria-label={image.title} className="fixed inset-0 z-50 flex flex-col bg-black/85 backdrop-blur-sm" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
      <div className="flex items-center gap-2 px-3 py-2 text-white/90">
        <span className="min-w-0 flex-1 truncate font-mono text-[12px]">{image.title}</span>
        {count > 1 && <span className="font-mono text-[11px] text-white/60">{index + 1} / {count}</span>}
        {image.downloadUrl && (
          <a href={image.downloadUrl} download className="inline-flex size-7 items-center justify-center rounded-md text-white/80 hover:bg-white/10" aria-label="Download" title="Download">
            <Download className="size-4" />
          </a>
        )}
        <IconButton label="Close" icon={<X className="size-4" />} onClick={onClose} className="text-white/80 hover:!bg-white/10 hover:!text-white" />
      </div>
      <div className="relative flex min-h-0 flex-1 items-center justify-center p-3" onMouseDown={(e) => e.target === e.currentTarget && onClose()}>
        <img src={image.src} alt={image.title} className="max-h-full max-w-full rounded object-contain shadow-2xl" />
        {count > 1 && (
          <>
            <button type="button" aria-label="Previous image" className="absolute left-2 top-1/2 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white hover:bg-black/70" onClick={() => onIndex((index - 1 + count) % count)}>
              <ChevronLeft className="size-5" />
            </button>
            <button type="button" aria-label="Next image" className="absolute right-2 top-1/2 -translate-y-1/2 rounded-full bg-black/50 p-2 text-white hover:bg-black/70" onClick={() => onIndex((index + 1) % count)}>
              <ChevronRight className="size-5" />
            </button>
          </>
        )}
      </div>
    </div>
  )
}
