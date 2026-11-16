import { useTheme } from '../theme'
import { Icon } from './Icon'

// ThemeToggle flips between light and dark; useTheme persists the choice and
// applies the tokens to :root. It shows the theme it switches to: a moon in
// light mode, a sun in dark mode, both drawn as outlines.
export function ThemeToggle() {
  const [name, setName] = useTheme()
  return (
    <button
      aria-label="Toggle theme"
      onClick={() => setName(name === 'dark' ? 'light' : 'dark')}
      className="rounded p-1.5 text-dim hover:text-fg"
    >
      <Icon size={20}>
        {name === 'dark' ? (
          <>
            <circle cx="12" cy="12" r="4" />
            <path d="M12 2v2M12 20v2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M2 12h2M20 12h2M4.9 19.1l1.4-1.4M17.7 6.3l1.4-1.4" />
          </>
        ) : (
          <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
        )}
      </Icon>
    </button>
  )
}
