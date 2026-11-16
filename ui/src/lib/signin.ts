// A tab DevTopo did not open itself has no access key, so the API answers 401
// (see internal/server/auth.go). Rather than a page that waits forever, it
// says how to get a signed-in tab.
const message =
  'This tab is not signed in to DevTopo. Open DevTopo from its icon in the tray or menu bar (Open DevTopo), and this page will work.'

export function watchSignIn() {
  const original = window.fetch.bind(window)
  let shown = false
  window.fetch = async (input, init) => {
    const res = await original(input, init)
    const url = typeof input === 'string' ? input : input instanceof URL ? input.pathname : input.url
    if (res.status === 401 && url.includes('/api/') && !shown) {
      shown = true
      const box = document.createElement('div')
      box.setAttribute('role', 'alert')
      box.textContent = message
      Object.assign(box.style, {
        position: 'fixed', inset: '0', display: 'flex', alignItems: 'center', justifyContent: 'center', padding: '2rem',
        background: 'var(--bg, #fff)', color: 'var(--fg, #141c1b)', font: '16px system-ui, sans-serif', textAlign: 'center', zIndex: '2147483647',
      })
      document.body.appendChild(box)
    }
    return res
  }
}
