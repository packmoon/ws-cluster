package wire

import (
	"io"
)

// MsgKill 通知连接下线
type MsgKill struct {
	header *MessageHeader
	PeerID string
}

// decode Decode
func (m *MsgKill) decode(r io.Reader) error {
	var err error
	if m.PeerID, err = ReadString(r); err != nil {
		return err
	}
	return nil
}

// encode Encode
func (m *MsgKill) encode(w io.Writer) error {
	var err error
	if err = WriteString(w, m.PeerID); err != nil {
		return err
	}
	return nil
}

// Header 头信息
func (m *MsgKill) Header() *MessageHeader {
	return &MessageHeader{m.header.ID, MsgTypeKill, ScopeClient, m.header.To}
}