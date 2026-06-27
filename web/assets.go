// Package web holds margin's embedded front-end assets and HTML templates,
// compiled into the single binary via go:embed.
package web

import "embed"

// Static holds the assets served under /static/ (design system CSS + comment widget JS).
//
//go:embed design-system.css widget.js
var Static embed.FS

// Templates holds the HTML templates: the page shell, the doc index, and the
// portable export.
//
//go:embed shell.html.tmpl index.html.tmpl export.html.tmpl error.html.tmpl partials.tmpl
var Templates embed.FS
