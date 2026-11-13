// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

//go:build !darwin && !linux

package artifact

import (
	"fmt"
	"io/fs"
	"os"
)

// RenameNoReplace renames from to to, failing with fs.ErrExist if to exists.
// Without an atomic no-replace rename on this platform, a directory created
// between the check and the rename can still be replaced if it is empty.
func RenameNoReplace(from, to string) error {
	if _, err := os.Lstat(to); err == nil {
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrExist}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("rename %s: %w", to, err)
	}
	return os.Rename(from, to)
}
