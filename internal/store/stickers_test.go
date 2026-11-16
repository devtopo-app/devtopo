package store_test

import (
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/store"
)

// A sticker keeps its place, size, picture and name, and one count per day:
// a new day starts at 0 while yesterday's count stays.
// Stickers used to be placed from the window's edge and are now placed from the
// page's, which starts after the 180px nav. They move once, staying where they were.
func TestStickersMoveOnceToThePagesOrigin(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetSetting(store.StickerOriginSetting, ""); err != nil {
		t.Fatal(err)
	}
	for id, x := range map[string]float64{"far": 400, "near": 100} {
		if err := st.PutSticker(store.Sticker{ID: id, Kind: "counter", Pic: "water", Name: id, X: x, Y: 50, W: 72}); err != nil {
			t.Fatal(err)
		}
	}
	st.Close()
	want := map[string]float64{"far": 220, "near": 4}
	for i := 0; i < 2; i++ { // the second open must not move them again
		st, err = store.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		list, err := st.ListStickers("2026-11-01")
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range list {
			if s.X != want[s.ID] || s.Y != 50 {
				t.Fatalf("open %d: %s at %v,%v", i, s.ID, s.X, s.Y)
			}
		}
		st.Close()
	}
}

// A sticker made after the move is already placed from the page's edge.
func TestNewDatabaseDoesNotMoveStickers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db.sqlite")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.PutSticker(store.Sticker{ID: "s", Kind: "counter", Pic: "water", Name: "s", X: 400, Y: 50, W: 72}); err != nil {
		t.Fatal(err)
	}
	st.Close()
	st, err = store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	list, _ := st.ListStickers("2026-11-01")
	if len(list) != 1 || list[0].X != 400 {
		t.Fatalf("list: %+v", list)
	}
}

func TestStickersKeepLayoutAndADailyCount(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	s := store.Sticker{ID: "a", Kind: "counter", Pic: "water", Name: "Water", X: 40, Y: 120, W: 104}
	if err := st.PutSticker(s); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStickerCount("a", "2026-10-30", 5); err != nil {
		t.Fatal(err)
	}
	s.X, s.Name = 300, "Water glasses"
	if err := st.PutSticker(s); err != nil { // moved and renamed
		t.Fatal(err)
	}

	got, err := st.ListStickers("2026-10-30")
	if err != nil || len(got) != 1 {
		t.Fatalf("list: %v %+v", err, got)
	}
	if g := got[0]; g.X != 300 || g.Y != 120 || g.W != 104 || g.Name != "Water glasses" || g.Pic != "water" || g.Kind != "counter" || g.Count != 5 {
		t.Fatalf("sticker: %+v", g)
	}
	if next, _ := st.ListStickers("2026-10-31"); next[0].Count != 0 {
		t.Fatalf("a new day starts at 0: %+v", next[0])
	}

	if err := st.DeleteSticker("a"); err != nil {
		t.Fatal(err)
	}
	if left, _ := st.ListStickers("2026-10-30"); len(left) != 0 {
		t.Fatalf("deleted: %+v", left)
	}
	// Its counts go with it, so a new sticker with the same id starts clean.
	_ = st.PutSticker(s)
	if again, _ := st.ListStickers("2026-10-30"); again[0].Count != 0 {
		t.Fatalf("counts must go with the sticker: %+v", again[0])
	}
}
