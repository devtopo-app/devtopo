package launch

import (
	"os"
	"path/filepath"
)

// Paths are where DevTopo keeps its data: the folder, the database and the
// log written when it runs without a terminal.
type Paths struct {
	Dir, DB, Log string
}

// Data returns where DevTopo keeps its data: ~/.devtopo.
func Data() Paths {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".devtopo")
	return Paths{Dir: dir, DB: filepath.Join(dir, "devtopo.sqlite"), Log: filepath.Join(dir, "devtopo.log")}
}
