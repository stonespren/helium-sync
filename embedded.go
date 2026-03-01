package heliumsync

import "embed"

// SetupScript contains the embedded setup script.
//
//go:embed scripts/setup.sh
var SetupScript embed.FS
