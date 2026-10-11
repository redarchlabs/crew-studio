import type { ButtonHTMLAttributes, ReactNode } from 'react'
import { Link, type LinkProps } from 'react-router'
import { LoaderCircle } from 'lucide-react'

export type ButtonVariant = 'primary' | 'secondary' | 'ghost' | 'danger'
export type ButtonSize = 'sm' | 'md'

type ButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant
  size?: ButtonSize
  icon?: ReactNode
  busy?: boolean
}

const VARIANTS: Record<ButtonVariant, string> = {
  primary: 'bg-accent text-accent-fg border-accent hover:brightness-110 font-semibold',
  secondary: 'bg-panel-2 text-fg border-line hover:border-line-strong hover:bg-panel',
  ghost: 'bg-transparent text-muted border-transparent hover:text-fg hover:bg-panel-2',
  danger: 'bg-danger-soft text-danger border-transparent hover:border-danger',
}

const SIZES: Record<ButtonSize, string> = {
  sm: 'h-7 px-2 text-[12px] gap-1.5',
  md: 'h-8 px-3 text-[13px] gap-2',
}

export function buttonClass(variant: ButtonVariant = 'secondary', size: ButtonSize = 'md'): string {
  return `inline-flex shrink-0 items-center justify-center rounded-md border whitespace-nowrap transition-colors disabled:cursor-not-allowed disabled:opacity-50 ${VARIANTS[variant]} ${SIZES[size]}`
}

/** A router link that looks like a button (avoids nesting buttons inside anchors). */
export function LinkButton({ variant, size, icon, children, className = '', ...rest }: LinkProps & { variant?: ButtonVariant; size?: ButtonSize; icon?: ReactNode }) {
  return (
    <Link className={`${buttonClass(variant, size)} ${className}`} {...rest}>
      {icon}
      {children}
    </Link>
  )
}

export function Button({ variant = 'secondary', size = 'md', icon, busy, children, className = '', disabled, type = 'button', ...rest }: ButtonProps) {
  return (
    <button
      type={type}
      disabled={disabled || busy}
      className={`${buttonClass(variant, size)} ${className}`}
      {...rest}
    >
      {busy ? <LoaderCircle className="size-3.5 animate-spin" /> : icon}
      {children}
    </button>
  )
}

type IconButtonProps = ButtonHTMLAttributes<HTMLButtonElement> & { label: string; icon: ReactNode; tone?: 'default' | 'danger' }

export function IconButton({ label, icon, tone = 'default', className = '', type = 'button', ...rest }: IconButtonProps) {
  const toneCls = tone === 'danger' ? 'hover:text-danger hover:bg-danger-soft' : 'hover:text-fg hover:bg-panel-2'
  return (
    <button
      type={type}
      aria-label={label}
      title={label}
      className={`inline-flex size-7 items-center justify-center rounded-md text-muted transition-colors disabled:opacity-40 ${toneCls} ${className}`}
      {...rest}
    >
      {icon}
    </button>
  )
}
