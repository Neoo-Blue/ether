// Package mediasafety isolates untrusted media from the console origin.
package mediasafety

import "net/http"

// SetHeaders permits media and subtitle bytes while preventing active upstream
// content from executing when a media URL is opened as a document.
func SetHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
}
