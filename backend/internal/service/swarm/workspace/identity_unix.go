//go:build !windows

package workspace

import (
	"fmt"
	"os"
	"reflect"
	"syscall"
)

// O_NONBLOCK prevents an exchanged FIFO from hanging between Lstat and open;
// O_NOFOLLOW rejects a final symlink even when its target remains inside Root.
func openRead(root *os.Root, name string) (*os.File, error) {
	return root.OpenFile(name, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
}

func fileIdentity(info os.FileInfo) (id string, links uint64, changed int64, err error) {
	s, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return "", 0, 0, ErrUnsupported
	}
	// Darwin names this field Ctimespec; Linux and other Unix systems use Ctim.
	v := reflect.ValueOf(s).Elem()
	c := v.FieldByName("Ctimespec")
	if !c.IsValid() {
		c = v.FieldByName("Ctim")
	}
	if !c.IsValid() {
		return "", 0, 0, ErrUnsupported
	}
	changed = c.FieldByName("Sec").Int()*1e9 + c.FieldByName("Nsec").Int()
	return fmt.Sprintf("%d:%d", s.Dev, s.Ino), uint64(s.Nlink), changed, nil
}
