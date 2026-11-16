// Package devtopo carries the repo-root files the app ships with.
package devtopo

import _ "embed"

// Guide is the user guide, shown in the app as its Help page. The README is
// the project's front page and links to it.
//
//go:embed GUIDE.md
var Guide string
