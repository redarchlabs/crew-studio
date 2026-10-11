import { toast } from 'sonner'
import { ApiError, errorMessage } from '../api/client'

/** Shows a toast for a failed action; 422 details are summarised in the description. */
export function notifyError(action: string, err: unknown): void {
  const description =
    err instanceof ApiError && err.details.length > 0
      ? err.details.slice(0, 4).join(' · ') + (err.details.length > 4 ? ' …' : '')
      : errorMessage(err)
  toast.error(action, { description })
}

export function notifySuccess(message: string): void {
  toast.success(message)
}
