import { useState } from 'react'
import { FoldVertical } from 'lucide-react'
import type { ChatMessage } from '../../api/types.chat'
import { Button } from '../../components/Button'
import { compact } from '../../lib/format'
import { conversationContext, HISTORY_MESSAGES } from './chatLogic'

type Props = {
  messages: readonly ChatMessage[]
  /** Set when no turn may start now (a reply is coming, chat is off, the budget is used up) */
  disabled: boolean
  onCompact: () => Promise<void>
}

/** How much of the conversation each message resends, and a button to compact it into a summary. */
export function CompactBar({ messages, disabled, onCompact }: Props) {
  const [busy, setBusy] = useState(false)
  const context = conversationContext(messages)
  if (messages.length === 0) return null

  const run = async () => {
    setBusy(true)
    try {
      await onCompact()
    } finally {
      setBusy(false)
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-2 text-[11.5px] text-faint">
      <span>
        Each message resends about {compact(context.tokens)} tokens of this conversation.
        {context.dropped > 0 && ` ${context.dropped} older ${context.dropped === 1 ? 'message no longer fits' : 'messages no longer fit'} (only the latest ${HISTORY_MESSAGES} are sent); compact to keep their gist.`}
      </span>
      <Button
        size="sm"
        variant="ghost"
        className="ml-auto"
        icon={<FoldVertical className="size-3.5" />}
        busy={busy}
        disabled={disabled || !context.canCompact}
        title={context.canCompact
          ? 'Summarise the conversation so far. Later messages send the summary instead of the full history, which costs fewer tokens.'
          : 'Nothing new to compact yet.'}
        onClick={() => void run()}
      >
        Compact
      </Button>
    </div>
  )
}
