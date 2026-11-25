package backend

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"github.com/egoist/mygo/plugins/sqlite"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

func TestNativeStoreMigratesNodeDataWithoutRuntime(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	old := filepath.Join(root, "backend-data")
	if err := os.MkdirAll(filepath.Join(old, "databases"), 0700); err != nil {
		t.Fatal(err)
	}
	db, err := sqlite.Open(ctx, filepath.Join(old, "settings.sqlite"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Execute(ctx, "CREATE TABLE nodejs_webstorage(key BLOB,value BLOB)"); err != nil {
		t.Fatal(err)
	}
	encode := func(s string) []byte {
		units := utf16.Encode([]rune(s))
		out := make([]byte, len(units)*2)
		for i, u := range units {
			binary.LittleEndian.PutUint16(out[i*2:], u)
		}
		return out
	}
	if _, err = db.Execute(ctx, "INSERT INTO nodejs_webstorage VALUES (?,?)", encode("users"), encode("[{\"login\":\"中文\"}]")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	db, err = sqlite.Open(ctx, filepath.Join(old, "databases", "D_^Database.sqlite"), sqlite.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Execute(ctx, "CREATE TABLE S_repositories(key BLOB,value BLOB)"); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Execute(ctx, "INSERT INTO S_repositories VALUES (?,?)", "id1", `{"id":1,"path":"C:/项目","alias":"名称","missing":false,"login":null}`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	s, err := OpenNativeStore(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	users, err := s.Get(ctx, "users")
	if err != nil || users != "[{\"login\":\"中文\"}]" {
		t.Fatalf("users %q %v", users, err)
	}
	repos, err := s.Repositories(ctx)
	if err != nil || len(repos) != 1 || repos[0].Path != "C:/项目" || *repos[0].Alias != "名称" {
		t.Fatalf("repos %+v %v", repos, err)
	}
	if err = s.Set(ctx, "theme", "dark"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = OpenNativeStore(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if theme, err := s.Get(ctx, "theme"); err != nil || theme != "dark" {
		t.Fatalf("persistent %q %v", theme, err)
	}
}

func TestNativeStoreReadsActualRetiredStorage(t *testing.T) {
	source := os.Getenv("GITOWL_LEGACY_FIXTURE")
	if source == "" {
		t.Skip("unified native validation generates the real legacy fixture")
	}
	ctx := context.Background()
	root := t.TempDir()
	before := map[string][32]byte{}
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		target := filepath.Join(root, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		before[rel] = sha256.Sum256(data)
		return os.WriteFile(target, data, 0600)
	})
	if err != nil {
		t.Fatal(err)
	}
	store, err := OpenNativeStore(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	repos, err := store.Repositories(ctx)
	if err != nil || len(repos) != 1 {
		t.Fatalf("repositories %v %v", repos, err)
	}
	r := repos[0]
	if r.Path != "C:/fixture/中文^name" || r.Alias == nil || *r.Alias != "名称^0" || r.Account == nil || r.Account.Endpoint != "https://api.github.com" || r.Account.Login != "fixture" {
		t.Fatalf("migration changed repository identity: %+v", r)
	}
	if theme, err := store.Get(ctx, "theme"); err != nil || theme != "dark" {
		t.Fatal("theme was not migrated")
	}
	rows, err := store.db.Query(ctx, "SELECT COUNT(*) FROM legacy_records")
	if err != nil || rows.Values[0][0].(int64) != 4 {
		t.Fatalf("structured records not retained: %+v %v", rows, err)
	}
	for rel, sum := range before {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || sha256.Sum256(data) != sum {
			t.Fatalf("original legacy file changed: %s", rel)
		}
	}
}

func TestNativeLegacyImportCanRetryAfterFailedExport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "desktop-kernel", "IndexedDB"), 0700); err != nil {
		t.Fatal(err)
	}
	if store, err := OpenNativeStore(ctx, root); err == nil {
		store.Close()
		t.Fatal("unexported Chromium data became an empty profile")
	}
	if err := prepareNativeLegacy(ctx, filepath.Join(root, "resources", "backend"), root); err == nil {
		t.Fatal("incomplete native database bypassed legacy export")
	}
	data := []byte(`{"format":1,"settings":{"theme":"dark"},"databases":[{"name":"Database","stores":[{"name":"repositories","keys":[1],"values":[{"id":1,"path":"C:/fixture"}]}]}]}`)
	if err := os.WriteFile(filepath.Join(root, "legacy-export.bin"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareNativeLegacy(ctx, filepath.Join(root, "resources", "backend"), root); err != nil {
		t.Fatal(err)
	}
	store, err := OpenNativeStore(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if marker, err := store.Get(ctx, "native-migration-v1"); err != nil || marker != "complete" {
		t.Fatal("retry did not commit migration")
	}
}
