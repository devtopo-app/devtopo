// The review states of your own PRs, with their copy and colour.
const states: Record<string, { label: string; color: string }> = {
  changes_requested: { label: 'Changes requested', color: 'var(--danger)' },
  commented: { label: 'Commented', color: 'var(--waiting)' },
  approved: { label: 'Approved', color: 'var(--done)' },
}

export const reviewLabel = (state: string) => states[state]?.label ?? ''

// ReviewBadge shows the state of one of your PRs; nothing for an unknown state.
export function ReviewBadge({ state }: { state: string }) {
  const s = states[state]
  if (!s) return null
  return (
    <span
      className="flex-none whitespace-nowrap rounded-full border px-1.5 text-2xs leading-4 text-fg"
      style={{ background: `color-mix(in srgb, ${s.color} 18%, var(--raised))`, borderColor: `color-mix(in srgb, ${s.color} 45%, var(--raised))` }}
    >
      {s.label}
    </span>
  )
}
