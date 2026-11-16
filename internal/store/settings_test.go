package store_test

import (
	"path/filepath"
	"testing"

	"github.com/devtopo-app/devtopo/internal/store"
)

func TestSettingsRoundTrip(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "db.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	if _, ok, err := st.GetSetting("linear_api_key"); err != nil || ok {
		t.Fatalf("absent setting should be ok=false, got ok=%v err=%v", ok, err)
	}
	if err := st.SetSetting("linear_api_key", "v1"); err != nil {
		t.Fatal(err)
	}
	v, ok, err := st.GetSetting("linear_api_key")
	if err != nil || !ok || v != "v1" {
		t.Fatalf("got %q ok=%v err=%v", v, ok, err)
	}
	if err := st.SetSetting("linear_api_key", "v2"); err != nil {
		t.Fatal(err)
	}
	if v, _, _ := st.GetSetting("linear_api_key"); v != "v2" {
		t.Fatalf("overwrite: %q", v)
	}
}
