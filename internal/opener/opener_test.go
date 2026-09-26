package opener

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOpenCommand(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opened")
	if err := Open([]string{"touch"}, path); err != nil {
		t.Fatal(err)
	}
	for range 100 {
		if _, err := os.Stat(path); err == nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("the command never ran on the target")
}
