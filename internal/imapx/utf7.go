package imapx

import utf7 "github.com/cention-sany/utf7"

// EncodeMailbox and DecodeMailbox make the QQ folder wire encoding explicit
// at the protocol boundary. go-imap applies the same Modified UTF-7 rules when
// serializing mailbox arguments.
func EncodeMailbox(value string) string { return utf7.UTF7Encode(value) }

func DecodeMailbox(value string) (string, error) { return utf7.UTF7Decode(value) }
