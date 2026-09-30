package source

import (
	"os"
	"path/filepath"
	"testing"
)

func TestZZReplay(t *testing.T) {
	files, _ := filepath.Glob("/tmp/claude-0/-home-user-quanto/d8cf5c5a-6ced-53ef-92a6-3d5bce617b31/scratchpad/last-*")
	for _, f := range files {
		data, _ := os.ReadFile(f)
		t.Logf("start %s", f)
		d, err := Load("x", data)
		t.Logf("loaded %s err=%v", f, err)
		if err == nil && !d.Empty() {
			d.Root().Walk(func(n *Node) bool { n.Pos(); n.Fields(); return true })
		}
		t.Logf("done %s", f)
	}
}
