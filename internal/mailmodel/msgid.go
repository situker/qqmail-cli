package mailmodel

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"

	"github.com/situker/qqmail-cli/internal/errmap"
)

type msgIDPayload struct {
	Folder      string `json:"f"`
	UIDValidity uint32 `json:"v"`
	UID         uint32 `json:"u"`
}

type MsgID struct {
	Folder      string
	UIDValidity uint32
	UID         uint32
}

func (id MsgID) String() string {
	raw, _ := json.Marshal(msgIDPayload(id))
	return "m1_" + base64.RawURLEncoding.EncodeToString(raw)
}

func ParseMsgID(value string) (MsgID, error) {
	if strings.HasPrefix(value, "m1_") {
		raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, "m1_"))
		if err != nil {
			return MsgID{}, invalidID(err)
		}
		var payload msgIDPayload
		if err := json.Unmarshal(raw, &payload); err != nil || payload.Folder == "" || payload.UID == 0 {
			return MsgID{}, invalidID(err)
		}
		return MsgID(payload), nil
	}
	// Accept the documented legacy shape for forward compatibility with early builds.
	parts := strings.Split(value, ":")
	if len(parts) >= 3 {
		uidValidity, err1 := strconv.ParseUint(parts[len(parts)-2], 10, 32)
		uid, err2 := strconv.ParseUint(parts[len(parts)-1], 10, 32)
		folder := strings.Join(parts[:len(parts)-2], ":")
		if err1 == nil && err2 == nil && folder != "" && uid > 0 {
			return MsgID{Folder: folder, UIDValidity: uint32(uidValidity), UID: uint32(uid)}, nil
		}
	}
	return MsgID{}, invalidID(nil)
}

func invalidID(cause error) error {
	return &errmap.Error{Kind: errmap.Usage, Message: "邮件 id 格式无效；请把 envelope list 返回的 id 原样传入", Cause: cause}
}

