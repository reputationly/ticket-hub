package web

import (
	"bytes"
	"io/fs"
	"os"
	"testing"
)

func TestEmbeddedAssets(t *testing.T) {
	err := fs.WalkDir(os.DirFS("."), "dist", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		want, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		got, err := Files.ReadFile(path)
		if err != nil {
			t.Errorf("静态资源未嵌入：%s：%v", path, err)
			return nil
		}
		if !bytes.Equal(got, want) {
			t.Errorf("静态资源内容不一致：%s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
