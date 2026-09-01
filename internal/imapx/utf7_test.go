package imapx

import "testing"

func TestModifiedUTF7RoundTrip(t *testing.T) {
	for _, value := range []string{"INBOX", "Sent Messages", "客户邮件", "研发&测试"} {
		encoded := EncodeMailbox(value)
		got, err := DecodeMailbox(encoded)
		if err != nil || got != value {
			t.Fatalf("%q -> %q -> %q (%v)", value, encoded, got, err)
		}
	}
}
