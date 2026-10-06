// Package assets embeds the application icons.
package assets

import _ "embed"

//go:embed app.ico
var AppIcon []byte

//go:embed app-unread.ico
var AppIconUnread []byte
