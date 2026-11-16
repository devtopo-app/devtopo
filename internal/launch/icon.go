package launch

import (
	"bytes"
	"embed"
	"encoding/binary"
	"fmt"
)

// iconSizes are the tray icon's sizes, in pixels: each one scaled from the
// logo (packaging/logo.png) by packaging/make-icons.py, so the system never
// has to scale it and every size stays sharp.
var iconSizes = []int{16, 24, 32, 48, 64}

//go:embed icons/*.png
var icons embed.FS

func iconPNGOf(px int) []byte {
	b, err := icons.ReadFile(fmt.Sprintf("icons/%d.png", px))
	if err != nil {
		panic(err) // the sizes are embedded at build time; this cannot fail
	}
	return b
}

// IconPNG is the tray icon for macOS and Linux: the 64 px logo, which the
// system shows smaller.
func IconPNG() []byte { return iconPNGOf(64) }

// IconICO is the tray icon for Windows: an ICO holding every size as a PNG
// (valid since Windows Vista), from which Windows picks the one that fits the
// screen's scale.
func IconICO() []byte {
	var b bytes.Buffer
	le := func(v any) { _ = binary.Write(&b, binary.LittleEndian, v) }
	le(uint16(0))              // reserved
	le(uint16(1))              // type: icon
	le(uint16(len(iconSizes))) // number of images
	offset := 6 + 16*len(iconSizes)
	pngs := make([][]byte, len(iconSizes))
	for i, px := range iconSizes {
		pngs[i] = iconPNGOf(px)
		b.Write([]byte{byte(px), byte(px), 0, 0}) // width, height, no palette, reserved
		le(uint16(1))                             // colour planes
		le(uint16(32))                            // bits per pixel
		le(uint32(len(pngs[i])))                  // image size
		le(uint32(offset))                        // image offset
		offset += len(pngs[i])
	}
	for _, p := range pngs {
		b.Write(p)
	}
	return b.Bytes()
}
