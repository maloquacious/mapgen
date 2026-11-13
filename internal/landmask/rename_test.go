// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package landmask

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// An empty directory created after Write's existence check must not be
// replaced by the final rename, as a plain os.Rename would do.
func TestRenameNoReplaceKeepsEmptyDirectory(t *testing.T) {
	root := t.TempDir()
	from, to := filepath.Join(root, "from"), filepath.Join(root, "to")
	for _, d := range []string{from, to} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(from, "marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := renameNoReplace(from, to); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("got %v, want fs.ErrExist", err)
	}
	if _, err := os.Stat(filepath.Join(to, "marker")); !os.IsNotExist(err) {
		t.Fatal("existing directory was replaced")
	}
	if err := renameNoReplace(from, filepath.Join(root, "new")); err != nil {
		t.Fatal(err)
	}
}
