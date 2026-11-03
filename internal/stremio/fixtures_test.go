package stremio

import "github.com/braedonsaunders/ether/internal/core"

const (
	hashA = "a584351126afcf50d32be068f3c9a62550d41ecc"
	hashB = "f0fd1cad0e24b69ff6aaadb9df40c50286327188"
	hashC = "1b42b99b9fbc0e0416a379787f250eb73b7434e3"
	hashD = "aa1a43787ff85fe5d029969857b0d64a6c9a05e4"
	// secret stands in for a debrid key inside an add-on's settings path.
	secret = "SECRETKEY123"
)

// convert is Convert with default options, in the shape the conversion
// tests read.
func convert(streams []Stream, req core.SearchRequest, packs bool) ([]core.Release, int, int) {
	c := Convert(streams, req, Lookup{Packs: packs}, ConvertOptions{})
	return c.Releases, c.SkippedNoHash, c.SkippedAmbiguous
}
