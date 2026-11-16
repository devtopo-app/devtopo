package launch

import (
	"bytes"
	"encoding/binary"
	"image/png"
	"testing"
)

func TestIconIsTheLogoAt64(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(IconPNG()))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 64 || b.Dy() != 64 {
		t.Fatalf("size %v", b)
	}
	if _, _, _, a := img.At(32, 32).RGBA(); a == 0 {
		t.Fatal("the logo must fill the middle")
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a != 0 {
		t.Fatal("the corners must be clear")
	}
}

// Windows wants an ICO; this one holds every size as a PNG, each entry
// pointing at its own image, so Windows can pick the sharp one.
func TestIconICOHoldsEverySize(t *testing.T) {
	ico := IconICO()
	n := int(binary.LittleEndian.Uint16(ico[4:]))
	if binary.LittleEndian.Uint16(ico[2:]) != 1 || n != len(iconSizes) {
		t.Fatalf("header: % x", ico[:6])
	}
	for i, px := range iconSizes {
		e := ico[6+16*i:]
		if int(e[0]) != px || int(e[1]) != px {
			t.Fatalf("entry %d is %dx%d, want %d", i, e[0], e[1], px)
		}
		size, off := binary.LittleEndian.Uint32(e[8:]), binary.LittleEndian.Uint32(e[12:])
		img, err := png.Decode(bytes.NewReader(ico[off : off+size]))
		if err != nil {
			t.Fatalf("entry %d: %v", i, err)
		}
		if img.Bounds().Dx() != px {
			t.Fatalf("entry %d holds a %d px image, want %d", i, img.Bounds().Dx(), px)
		}
	}
}
