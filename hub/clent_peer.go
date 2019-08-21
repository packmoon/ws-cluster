package hub

import (
	"time"

	"github.com/gorilla/websocket"
	"github.com/ws-cluster/peer"
	"github.com/ws-cluster/wire"
)

// ClientPeer 代表一个客户端节点，消息收发的处理逻辑
type ClientPeer struct {
	*peer.Peer
	Addr    wire.Addr
	LoginAt time.Time //second

	msgchan   chan<- *Packet
	closechan chan<- *delPeer
}

// OnMessage 接收消息
func (p *ClientPeer) OnMessage(message *wire.Message) error {
	err := make(chan error)
	p.msgchan <- &Packet{from: fromClient, fromID: p.ID, message: message, err: err}

	// header := message.Header

	// if !header.Dest.IsEmpty() {
	// 	log.Printf("message %v to %v , Type: %v", header.Source.String(), header.Dest.String(), header.Command)
	// 	// errchan := make(chan error)
	// 	// 消息转发

	// } else {
	// 	p.hub.commandChan <- message
	// }

	return <-err
}

// OnDisconnect 接连断开
func (p *ClientPeer) OnDisconnect() error {
	p.closechan <- &delPeer{peer: p, done: nil}
	return nil
}

func newClientPeer(addr wire.Addr, remoteAddr string, h *Hub, conn *websocket.Conn) (*ClientPeer, error) {
	clientPeer := &ClientPeer{
		msgchan:   h.msgQueue,
		closechan: h.unregister,
		Addr:      addr,
	}

	peer := peer.NewPeer(addr.String(), remoteAddr, &peer.Config{
		Listeners: &peer.MessageListeners{
			OnMessage:    clientPeer.OnMessage,
			OnDisconnect: clientPeer.OnDisconnect,
		},
		MaxMessageSize: h.config.Peer.MaxMessageSize,
	})

	clientPeer.Peer = peer
	clientPeer.SetConnection(conn)

	return clientPeer, nil
}
