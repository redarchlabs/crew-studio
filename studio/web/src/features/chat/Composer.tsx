import { useState, type KeyboardEvent } from 'react'
import { SendHorizontal, Upload } from 'lucide-react'
import { Button } from '../../components/Button'
import { AttachButton, CHAT_FILE_ACCEPT, CHAT_FILE_LIMITS, chatFileReason, PendingFileList, usePendingFiles } from './pendingFiles'

/** Same limit as the server (model.MaxChatMessageLen) */
export const MAX_MESSAGE = 4000

type Props = {
  disabled?: boolean
  /** Why sending is not possible, shown instead of the hint */
  reason?: string
  placeholder?: string
  /** The hint's last sentence: what waits for the person's Confirm */
  confirmHint?: string
  /** Resolves true when the message was accepted, so the box (and its files) can be cleared */
  onSend: (text: string, files: File[]) => Promise<boolean>
}

/** The message box: Enter sends, Shift+Enter starts a new line. Screenshots can be pasted, and images and PDFs
 * dropped or attached, to go with the message. */
export function Composer({ disabled = false, reason, placeholder = 'Message the crew…', confirmHint = 'Changes the crew proposes wait for your Confirm.', onSend }: Props) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const pending = usePendingFiles({ limits: CHAT_FILE_LIMITS, refuse: chatFileReason })
  const trimmed = text.trim()
  const tooLong = text.length > MAX_MESSAGE
  const blocked = disabled || sending || (!trimmed && pending.files.length === 0) || tooLong

  const send = async () => {
    if (blocked) return
    setSending(true)
    try {
      if (await onSend(trimmed, pending.files)) {
        setText('')
        pending.clear()
      }
    } finally {
      setSending(false)
    }
  }
  const onKey = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    if (e.key === 'Enter' && !e.shiftKey) {
      e.preventDefault()
      void send()
    }
  }

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-end gap-2">
        <div className={`relative flex-1 rounded-md ${pending.dragging ? 'outline-2 outline-offset-2 outline-dashed outline-accent' : ''}`} {...(disabled ? {} : pending.dragProps)}>
          <textarea
            aria-label="Message"
            className="field-input min-h-[44px] w-full resize-y"
            rows={2}
            value={text}
            disabled={disabled}
            placeholder={placeholder}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKey}
            onPaste={pending.onPaste}
          />
          {pending.dragging && (
            <div className="pointer-events-none absolute inset-0 flex items-center justify-center rounded-md bg-bg/70 text-[13px] font-medium text-accent">
              <Upload className="mr-2 size-4" /> Drop images or PDFs
            </div>
          )}
        </div>
        <AttachButton pending={pending} accept={CHAT_FILE_ACCEPT} disabled={disabled || sending} />
        <Button variant="primary" icon={<SendHorizontal className="size-4" />} busy={sending} disabled={blocked} onClick={() => void send()}>
          Send
        </Button>
      </div>
      <PendingFileList pending={pending} disabled={sending} />
      <p className={`text-[11.5px] ${tooLong || reason ? 'text-danger' : 'text-faint'}`}>
        {reason ?? (tooLong ? `At most ${MAX_MESSAGE.toLocaleString()} characters (${text.length.toLocaleString()} now).` : `Enter sends, Shift+Enter adds a line. Paste screenshots or drop images and PDFs to send them along. ${confirmHint}`)}
      </p>
    </div>
  )
}
