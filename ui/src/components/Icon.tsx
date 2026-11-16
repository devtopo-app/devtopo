import type { ReactNode } from 'react'

// Icon draws a 24-unit line icon (outline only) in the current text colour.
export function Icon({ children, size = 18 }: { children: ReactNode; size?: number }) {
  return (
    <svg aria-hidden viewBox="0 0 24 24" width={size} height={size} fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className="shrink-0">
      {children}
    </svg>
  )
}
