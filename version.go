// Copyright (c) 2026 Michael D Henderson.
// SPDX-License-Identifier: AGPL-3.0-or-later

package mapgen

import (
	"github.com/maloquacious/semver"
)

var (
	version = semver.Version{
		Major:      0,
		Minor:      4,
		Patch:      0,
		PreRelease: "alpha",
	}
)

// Version returns the version of this library. World.Version records its
// String form, such as "1.0.0".
func Version() semver.Version {
	return version
}
