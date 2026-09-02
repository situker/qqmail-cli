package mailmodel

import (
	"mime"
	"net/mail"
	"strings"

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

var lenientAddressParser = mail.AddressParser{WordDecoder: &headerWordDecoder}

// ParseAddressListLenient parses a raw address header with charset-aware
// RFC 2047 display-name decoding. It never fails: a header RFC 5322 cannot
// parse is preserved as a decoded display name rather than silently dropped —
// wild Chinese email regularly carries malformed address headers.
func ParseAddressListLenient(value string) []Address {
	value = strings.TrimSpace(value)
	if value == "" {
		return []Address{}
	}
	parsed, err := lenientAddressParser.ParseList(value)
	if err != nil {
		return []Address{{Name: DecodeHeaderText(value)}}
	}
	result := make([]Address, 0, len(parsed))
	for _, address := range parsed {
		result = append(result, Address{Name: DecodeHeaderText(address.Name), Email: address.Address})
	}
	return result
}
