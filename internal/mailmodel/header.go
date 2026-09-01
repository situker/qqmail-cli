package mailmodel

import (
	"mime"

	htmlcharset "golang.org/x/net/html/charset"
)

var headerWordDecoder = mime.WordDecoder{CharsetReader: htmlcharset.NewReaderLabel}

// DecodeHeaderText performs a tolerant second RFC 2047 decoding pass. Some
// providers return an encoded-word as the already-parsed display name (for
// example when it was quoted in the original address header).
func DecodeHeaderText(value string) string {
	decoded, err := headerWordDecoder.DecodeHeader(value)
	if err != nil {
		return value
	}
	return decoded
}
