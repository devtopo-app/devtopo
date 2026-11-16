import logo from '../assets/logo-64.png'

// Logo is DevTopo's logo, the mouse reading its map, beside the name. It is
// made from packaging/logo.png by packaging/make-icons.py, like the tray and
// tab icons.
export function Logo({ size = 28 }: { size?: number }) {
  return <img src={logo} width={size} height={size} alt="" aria-hidden="true" />
}
