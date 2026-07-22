package webui

import "embed"

// Assets contains the dashboard so the release remains a single binary.
//
//go:embed index.html styles.css app.js
var Assets embed.FS
