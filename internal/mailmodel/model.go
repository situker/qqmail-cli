package mailmodel

import "time"

type Address struct {
	Name  string `json:"name,omitempty"`
	Email string `json:"email"`
}

type Folder struct {
	Name       string   `json:"name"`
	Delimiter  string   `json:"delimiter,omitempty"`
	Attributes []string `json:"attributes"`
}

type Envelope struct {
	ID             string    `json:"id"`
	UID            uint32    `json:"uid"`
	UIDValidity    uint32    `json:"uidvalidity"`
	Folder         string    `json:"folder"`
	Subject        string    `json:"subject"`
	From           []Address `json:"from"`
	To             []Address `json:"to"`
	Date           time.Time `json:"date"`
	InternalDate   time.Time `json:"internal_date"`
	Size           int64     `json:"size_bytes"`
	Flags          []string  `json:"flags"`
	HasAttachments bool      `json:"has_attachments"`
}

// HeaderFields contains the small set of message headers needed by the local
// rule engine. Values originate from email and are therefore untrusted data.
type HeaderFields struct {
	UID             uint32 `json:"uid"`
	MessageID       string `json:"message_id"`
	ListUnsubscribe string `json:"list_unsubscribe"`
	Precedence      string `json:"precedence"`
}

type Attachment struct {
	Index       int    `json:"index"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size_bytes"`
	ContentID   string `json:"content_id,omitempty"`
	Data        []byte `json:"-"`
}

type Message struct {
	Envelope    Envelope     `json:"envelope"`
	Text        *string      `json:"text"`
	HTML        *string      `json:"html"`
	Raw         []byte       `json:"-"`
	Attachments []Attachment `json:"attachments"`
	Parser      string       `json:"parser"`
}
