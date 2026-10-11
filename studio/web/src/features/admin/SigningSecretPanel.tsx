import { useState } from 'react'
import { Eye, EyeOff, RotateCw } from 'lucide-react'
import { adminApi } from '../../api/endpoints'
import { Button } from '../../components/Button'
import { Panel } from '../../components/PageHeader'
import { ConfirmButton, CopyButton } from '../../components/misc'
import { notifyError, notifySuccess } from '../../lib/notify'

/** The service account's callback signing secret: fetched only when asked for, copyable and rotatable. */
export function SigningSecretPanel({ userId }: { userId: string }) {
  const [secret, setSecret] = useState<string | null>(null)
  const [busy, setBusy] = useState<'reveal' | 'rotate' | null>(null)

  const reveal = async () => {
    setBusy('reveal')
    try {
      setSecret((await adminApi.users.signingSecret(userId)).signing_secret)
    } catch (err) {
      notifyError('Could not load the signing secret', err)
    } finally {
      setBusy(null)
    }
  }

  const rotate = async () => {
    setBusy('rotate')
    try {
      setSecret((await adminApi.users.rotateSigningSecret(userId)).signing_secret)
      notifySuccess('Signing secret rotated. Update the systems that verify callbacks.')
    } catch (err) {
      notifyError('Could not rotate the signing secret', err)
    } finally {
      setBusy(null)
    }
  }

  return (
    <Panel title="Callback signing secret">
      <p className="mb-3 text-[12px] text-muted">
        Signs run callbacks sent to this account&apos;s <code className="font-mono">callback_url</code> (header{' '}
        <code className="font-mono">X-CrewStudio-Signature</code>), so the receiver can check they came from Crew Studio.
      </p>
      {secret ? (
        <div className="mb-3 flex items-center gap-1 rounded-md border border-line bg-raised p-2">
          <code className="min-w-0 flex-1 break-all font-mono text-[11.5px]">{secret}</code>
          <CopyButton text={secret} />
        </div>
      ) : (
        <div className="mb-3 rounded-md border border-dashed border-line px-3 py-2 font-mono text-[12px] tracking-widest text-faint">••••••••••••••••</div>
      )}
      <div className="flex flex-wrap gap-2">
        {secret ? (
          <Button size="sm" variant="ghost" icon={<EyeOff className="size-3.5" />} onClick={() => setSecret(null)}>Hide</Button>
        ) : (
          <Button size="sm" icon={<Eye className="size-3.5" />} busy={busy === 'reveal'} onClick={() => void reveal()}>Reveal</Button>
        )}
        <ConfirmButton icon={<RotateCw className="size-3.5" />} busy={busy === 'rotate'} onConfirm={() => void rotate()}>Rotate</ConfirmButton>
      </div>
      <p className="mt-2 text-[11.5px] text-faint">Rotating takes effect at once: callbacks signed with the old secret will fail verification.</p>
    </Panel>
  )
}
