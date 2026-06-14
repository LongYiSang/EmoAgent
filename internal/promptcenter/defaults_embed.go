package promptcenter

import "embed"

//go:embed defaults/components.yaml defaults/*.md
var defaultsFS embed.FS
