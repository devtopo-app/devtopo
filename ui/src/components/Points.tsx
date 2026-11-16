// Points lists a session's summary points. A point that starts with its time
// ("09:12 Fixed ...") shows that time in a column of its own, so the list
// reads as a timeline; any other point is a plain bullet.
export function Points({ points, className = '' }: { points: string[]; className?: string }) {
  return (
    <ul className={`flex flex-col gap-0.5 ${className}`}>
      {points.map((p, i) => {
        const m = /^(\d{1,2}:\d{2})\s+(.*)$/.exec(p)
        return m ? (
          <li key={i} className="flex gap-2">
            <time className="shrink-0 pt-px text-xs tabular-nums text-dim">{m[1]}</time>
            <span className="min-w-0">{m[2]}</span>
          </li>
        ) : (
          <li key={i} className="ml-5 list-disc">
            {p}
          </li>
        )
      })}
    </ul>
  )
}
