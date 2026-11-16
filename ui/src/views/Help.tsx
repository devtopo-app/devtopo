import { useEffect, useState } from 'react'
import { marked } from 'marked'

// Help shows the setup guide: the GUIDE.md the app was built with, so the guide
// on GitHub and here is the same text. It is the app's own file, not user
// input, so rendering its HTML is safe. Outside links open in a new tab.
export function Help() {
  const [html, setHtml] = useState('')
  const [error, setError] = useState(false)
  useEffect(() => {
    fetch('/api/help')
      .then((r) => (r.ok ? r.text() : Promise.reject()))
      .then((md) => setHtml((marked.parse(md) as string).replace(/<a href="http/g, '<a target="_blank" rel="noreferrer" href="http')))
      .catch(() => setError(true))
  }, [])
  if (error) return <p className="text-dim">Could not load the help. Is `devtopo app` running?</p>
  return <article className="help max-w-3xl" dangerouslySetInnerHTML={{ __html: html }} />
}
