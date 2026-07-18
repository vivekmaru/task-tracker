package web

import _ "embed"

//go:embed assets/htmx-2.0.4.min.js
var htmxAsset []byte

//go:embed assets/favicon.svg
var faviconAsset []byte
