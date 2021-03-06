package hub

import (
	"bytes"
	"container/list"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	// cmap "github.com/orcaman/concurrent-map"
	"github.com/ws-cluster/database"
	"github.com/ws-cluster/filelog"
	"github.com/ws-cluster/wire"
)

const (
	pingInterval = time.Second * 3

	useForAddClientPeer = uint8(1)
	useForDelClientPeer = uint8(2)
	useForAddServerPeer = uint8(3)
	useForDelServerPeer = uint8(4)
	useForRelayMessage  = uint8(5)
)

var (
	// ErrPeerNoFound peer is not in this server
	ErrPeerNoFound = errors.New("peer is not in this server")
)

// Resp Resp
type Resp struct {
	Status uint8
	Err    error
	Body   wire.Protocol
}

// Packet  Packet to hub
type Packet struct {
	from    wire.Addr //
	use     uint8
	content interface{}
	resp    chan *Resp
}

// Server 服务器对象
type Server struct {
	Addr               wire.Addr // logic address
	AdvertiseClientURL *url.URL
	AdvertiseServerURL *url.URL
	Token              string
}

// Hub 是一个服务中心，所有 clientPeer
type Hub struct {
	upgrader *websocket.Upgrader
	config   *Config
	Server   *Server // self
	// clientPeers 缓存客户端节点数据
	clientPeers map[wire.Addr]*ClientPeer
	// serverPeers 缓存服务端节点数据
	serverPeers map[wire.Addr]*ServerPeer
	groups      map[wire.Addr]*Group
	location    map[wire.Addr]wire.Addr // client location in server

	messageLog *filelog.FileLog

	packetQueue     chan *Packet
	packetRelay     chan *Packet
	packetRelayDone chan *Packet
	quit            chan struct{}
}

// NewHub 创建一个 Server 对象，并初始化
func NewHub(conf *Config) (*Hub, error) {
	var upgrader = &websocket.Upgrader{
		ReadBufferSize:  conf.cpc.MaxMessageSize,
		WriteBufferSize: conf.cpc.MaxMessageSize,
		CheckOrigin: func(r *http.Request) bool {
			if conf.sc.Origins == "*" {
				return true
			}
			rOrigin := r.Header.Get("Origin")
			if strings.Contains(conf.sc.Origins, rOrigin) {
				return true
			}
			log.Println("refuse", rOrigin)
			return false
		},
	}

	var messageLog *filelog.FileLog
	if conf.ms != nil {
		messageLogConfig := &filelog.Config{
			File: conf.sc.MessageFile,
			SubFunc: func(msgs []*bytes.Buffer) error {
				return saveMessagesToDb(conf.ms, msgs)
			},
		}
		messageLog, _ = filelog.NewFileLog(messageLogConfig)
	}

	serverAddr, _ := wire.NewServerAddr(0, conf.sc.ID)

	hub := &Hub{
		upgrader:        upgrader,
		config:          conf,
		clientPeers:     make(map[wire.Addr]*ClientPeer, 10000),
		serverPeers:     make(map[wire.Addr]*ServerPeer, 10),
		location:        make(map[wire.Addr]wire.Addr, 10000),
		groups:          make(map[wire.Addr]*Group, 100),
		packetQueue:     make(chan *Packet, 1),
		packetRelay:     make(chan *Packet, 1),
		packetRelayDone: make(chan *Packet, 1),
		messageLog:      messageLog,
		quit:            make(chan struct{}),
		Server: &Server{
			Addr:               *serverAddr,
			Token:              conf.sc.ServerToken,
			AdvertiseClientURL: conf.sc.AdvertiseClientURL,
			AdvertiseServerURL: conf.sc.AdvertiseServerURL,
		},
	}

	go httplisten(hub, &conf.sc)

	log.Printf("server[%v] start up", serverAddr.String())

	return hub, nil
}

// Run start all handlers
func (h *Hub) Run() {
	err := h.startCluster()
	if err != nil {
		log.Println(err)
	}
	go h.packetHandler()
	go h.packetQueueHandler()

	<-h.quit
}

// 与其它服务器节点建立长连接
func (h *Hub) startCluster() error {
	if h.config.sc.ClusterSeedURL == "" {
		return nil
	}
	log.Println("start outPeerhandler")

	resp, err := http.Get(fmt.Sprintf("%v/q/servers", h.config.sc.ClusterSeedURL))
	if err != nil {
		return err
	}
	coder := json.NewDecoder(resp.Body)
	var servers []wire.Server
	if err := coder.Decode(&servers); err != nil {
		return err
	}

	// 主动连接到其它节点
	for _, server := range servers {
		if server.Addr == h.Server.Addr.String() {
			continue
		}
		curl, _ := url.Parse(server.ClientURL)
		surl, _ := url.Parse(server.ServerURL)
		serverPeer, err := newServerPeer(h, &Server{
			Addr:               *wire.ParseCorrectAddr(server.Addr),
			AdvertiseClientURL: curl,
			AdvertiseServerURL: surl,
		})
		if err != nil {
			log.Println(err)
			continue
		}

		h.serverPeers[serverPeer.Addr] = serverPeer
		log.Println("connected to server", server)
	}

	log.Println("end outPeerhandler")
	return nil
}

// 处理消息queue
func (h *Hub) packetQueueHandler() {
	log.Println("start packetQueueHandler")
	pendingMsgs := list.New()

	// We keep the waiting flag so that we know if we have a pending message
	waiting := false

	// To avoid duplication below.
	queuePacket := func(packet *Packet, list *list.List, waiting bool) bool {
		if !waiting {
			h.packetRelay <- packet
		} else {
			list.PushBack(packet)
		}
		// log.Println("panding message ", list.Len())
		// we are always waiting now.
		return true
	}
	for {
		select {
		case packet := <-h.packetQueue:
			if h.messageLog != nil && packet.use == useForRelayMessage {
				message := packet.content.(*wire.Message)
				buf := &bytes.Buffer{}
				message.Encode(buf)
				err := h.messageLog.Write(buf.Bytes())
				if err != nil {
					packet.resp <- &Resp{
						Status: wire.MsgStatusException,
						Err:    err,
					}
					continue
				}
			}
			waiting = queuePacket(packet, pendingMsgs, waiting)
		case <-h.packetRelayDone:
			// log.Printf("message %v relayed \n", ID)
			next := pendingMsgs.Front()
			if next == nil {
				waiting = false
				continue
			}
			val := pendingMsgs.Remove(next)
			h.packetRelay <- val.(*Packet)
		}
	}
}

func (h *Hub) packetHandler() {
	log.Println("start packetHandler")
	for {
		select {
		case packet := <-h.packetRelay:
			switch packet.use {
			case useForAddClientPeer:
				h.handleClientPeerRegistPacket(packet.from, packet.content.(*ClientPeer), packet.resp)
			case useForDelClientPeer:
				h.handleClientPeerUnregistPacket(packet.from, packet.content.(*ClientPeer), packet.resp)
			case useForAddServerPeer:
				h.handleServerPeerRegistPacket(packet.from, packet.content.(*ServerPeer), packet.resp)
			case useForDelServerPeer:
				h.handleServerPeerUnregistPacket(packet.from, packet.content.(*ServerPeer), packet.resp)
			case useForRelayMessage:
				message := packet.content.(*wire.Message)
				header := message.Header
				h.recordSession(packet.from, header)
				if packet.from.Type() == wire.AddrServer && header.Source.Type() == wire.AddrClient { //如果是转发过来的消息，就记录发送者的定位
					h.recordLocation(packet.from, message)
				}
				if header.Dest == h.Server.Addr { // if dest address is self
					h.handleLogicPacket(packet.from, message, packet.resp)
				} else {
					h.handleRelayPacket(packet.from, message, packet.resp)
				}
			}

			h.packetRelayDone <- packet
		}
	}
}

func (h *Hub) recordSession(from wire.Addr, header *wire.Header) {
	if header.Source.Type() == wire.AddrClient {
		if speer, has := h.clientPeers[header.Source]; has {
			speer.AddSession(header.Dest, h.Server.Addr)
		}
	}
	if header.Dest.Type() != wire.AddrClient {
		return
	}
	if peer, has := h.clientPeers[header.Dest]; has {
		if from.Type() == wire.AddrClient { // source and dest peer are in same server
			peer.AddSession(header.Source, h.Server.Addr)
		} else {
			peer.AddSession(header.Source, from)
		}
	}
}

// record visiting client peer location if this message is relaid by a server peer
func (h *Hub) recordLocation(from wire.Addr, message *wire.Message) {
	header := message.Header
	if _, has := h.location[header.Source]; has {
		return
	}
	dest := header.Dest
	h.location[header.Source] = from
	// A locating message is sent to the source server if dest is in this server, let it know the dest client is in this server.
	// so the server can directly send the same dest message to this server on next time
	if _, has := h.clientPeers[dest]; has {
		loc := wire.MakeEmptyHeaderMessage(wire.MsgTypeLoc, &wire.MsgLoc{
			Target: header.Source,
			Peer:   dest,
			In:     h.Server.Addr,
		})
		loc.Header.Dest = from
		loc.Header.Source = h.Server.Addr
		if speer, has := h.serverPeers[from]; has {
			speer.PushMessage(loc, nil)
		}
	}
}

func (h *Hub) handleClientPeerRegistPacket(from wire.Addr, peer *ClientPeer, resp chan<- *Resp) {
	packet := wire.MakeEmptyHeaderMessage(wire.MsgTypeKill, &wire.MsgKill{
		LoginAt: uint64(time.Now().UnixNano() / 1000000),
	})
	packet.Header.Source = peer.Addr
	packet.Header.Dest = peer.Addr // same addr

	if oldpeer, ok := h.clientPeers[peer.Addr]; ok {
		oldpeer.PushMessage(packet, nil)
	}
	h.broadcast(packet) // 广播此消息到其它服务器节点

	h.clientPeers[peer.Addr] = peer

	if resp != nil {
		resp <- &Resp{Status: wire.MsgStatusOk}
	}
	return
}

func (h *Hub) handleClientPeerUnregistPacket(from wire.Addr, peer *ClientPeer, resp chan<- *Resp) {
	if alivePeer, ok := h.clientPeers[peer.Addr]; ok {
		if alivePeer.RemoteAddr != peer.RemoteAddr { // this two peer are different connection, ignore unregister
			resp <- &Resp{Status: wire.MsgStatusOk}
			return
		}
		delete(h.clientPeers, peer.Addr)

		// leave groups
		alivePeer.Groups.Each(func(elem interface{}) bool {
			gAddr := elem.(wire.Addr)
			if group, has := h.groups[gAddr]; has {
				group.packet <- &GroupPacket{useForLeave, peer}
			}
			return false
		})

		// notice other server your are offline
		for server, peers := range alivePeer.getAllSessionServers() {
			offline := wire.MakeEmptyHeaderMessage(wire.MsgTypeOffline, &wire.MsgOffline{
				Peer:    peer.Addr,
				Targets: peers,
				Notice:  peer.OfflineNotice,
			})
			offline.Header.Source = h.Server.Addr

			if speer, has := h.serverPeers[server]; has {
				offline.Header.Dest = server
				speer.PushMessage(offline, nil)
			} else {
				offline.Header.Dest = h.Server.Addr // send to logic handler
				// the session is in local server
				h.packetQueue <- &Packet{
					from:    h.Server.Addr,
					use:     useForRelayMessage,
					content: offline,
				}
			}
		}
	}
	if resp != nil {
		resp <- &Resp{Status: wire.MsgStatusOk}
	}
	return
}

func (h *Hub) handleServerPeerRegistPacket(from wire.Addr, peer *ServerPeer, resp chan<- *Resp) {
	h.serverPeers[peer.Addr] = peer
	if resp != nil {
		resp <- &Resp{Status: wire.MsgStatusOk}
	}
	return
}

func (h *Hub) handleServerPeerUnregistPacket(from wire.Addr, peer *ServerPeer, resp chan<- *Resp) {
	delete(h.serverPeers, peer.Addr)
	if resp != nil {
		resp <- &Resp{Status: wire.MsgStatusOk}
	}
	return
}

func (h *Hub) handleRelayPacket(from wire.Addr, message *wire.Message, resp chan<- *Resp) {
	header := message.Header
	dest := header.Dest
	var response = Resp{
		Status: wire.MsgStatusOk,
	}
	defer func() {
		if resp != nil {
			resp <- &response
		}
	}()
	if dest.Type() == wire.AddrClient {
		// 在当前服务器节点中找到了目标客户端
		if cpeer, ok := h.clientPeers[dest]; ok {
			cpeer.PushMessage(message, nil) //errchan pass to peer
			return
		}
		if from.Type() == wire.AddrServer { //dest no found in this server .then throw out message
			response.Err = ErrPeerNoFound
			return
		}
		// message sent from client directly
		serverAddr, has := h.location[dest]
		if !has { // 如果找不到定位，广播此消息
			h.broadcast(message)
		} else {
			if speer, ok := h.serverPeers[serverAddr]; ok {
				speer.PushMessage(message, nil)
			}
		}
		response.Err = ErrPeerNoFound
	} else {
		// 如果消息是直接来源于 client。就转发到其它服务器
		if from.Type() == wire.AddrClient {
			h.broadcast(message)
		}

		if dest.Type() == wire.AddrGroup {
			// 消息异步发送到群中所有用户
			if group, has := h.groups[dest]; has {
				group.packet <- &GroupPacket{useForMessage, message}
			}
		} else if dest.Type() == wire.AddrBroadcast {
			// 消息异步发送到群中所有用户
			h.sendToDomain(dest, message)
		}
	}
}

func (h *Hub) handleLogicPacket(from wire.Addr, message *wire.Message, resp chan<- *Resp) {
	header := message.Header
	body := message.Body
	var response = Resp{
		Status: wire.MsgStatusOk,
	}
	defer func() {
		if resp != nil {
			resp <- &response
		}
	}()

	switch header.Command {
	case wire.MsgTypeGroupInOut:
		msgGroup := body.(*wire.MsgGroupInOut)
		peer := h.clientPeers[header.Source]
		if peer == nil {
			response.Err = ErrPeerNoFound
			return
		}
		for _, group := range msgGroup.Groups {
			switch msgGroup.InOut {
			case wire.GroupIn:
				peer.Groups.Add(group) //record to peer
				if _, ok := h.groups[group]; !ok {
					h.groups[group] = NewGroup(group, h.config.sc.GroupBufferSize)
				}
				h.groups[group].packet <- &GroupPacket{useForJoin, peer}
			case wire.GroupOut:
				peer.Groups.Remove(group)
				if g, has := h.groups[group]; has {
					g.packet <- &GroupPacket{useForLeave, peer}

					if g.MemCount == 0 {
						if len(h.groups) > 1000 { // clean group
							g.Exit() //stop
							delete(h.groups, group)
						}
					}
				}

			}
		}
	case wire.MsgTypeLoc: //handle location message
		msgLoc := body.(*wire.MsgLoc)
		h.location[msgLoc.Peer] = msgLoc.In
		//  regist a server to peer whether it is successful
		peer := h.clientPeers[msgLoc.Target]
		peer.AddSession(msgLoc.Peer, msgLoc.In)
	case wire.MsgTypeOffline: //handle offline message
		msgOffline := body.(*wire.MsgOffline)
		delete(h.location, msgOffline.Peer)

		for _, target := range msgOffline.Targets {
			peer, has := h.clientPeers[target]
			if !has {
				continue
			}
			peer.DelSession(msgOffline.Peer)
			if msgOffline.Notice == 1 { //notice to client
				offlineNotice := wire.MakeEmptyHeaderMessage(wire.MsgTypeOfflineNotice, &wire.MsgOfflineNotice{
					Peer: msgOffline.Peer,
				})
				offlineNotice.Header.Dest = target
				peer.PushMessage(offlineNotice, nil)
			}
		}
	case wire.MsgTypeQueryClient:
		query := body.(*wire.MsgQueryClient)
		var msgResp = new(wire.MsgQueryClientResp)
		if peer, has := h.clientPeers[query.Peer]; has {
			msgResp.LoginAt = uint32(peer.LoginAt.Unix())
		}
		response.Body = msgResp
	case wire.MsgTypeQueryServers:
		msgresp := new(wire.MsgQueryServersResp)
		msgresp.Servers = append(msgresp.Servers, wire.Server{
			Addr:      h.Server.Addr.String(),
			ClientURL: h.Server.AdvertiseClientURL.String(),
			ServerURL: h.Server.AdvertiseServerURL.String(),
		})

		for _, speer := range h.serverPeers {
			msgresp.Servers = append(msgresp.Servers, wire.Server{
				Addr:      speer.Server.Addr.String(),
				ClientURL: speer.Server.AdvertiseClientURL.String(),
				ServerURL: speer.Server.AdvertiseServerURL.String(),
			})
		}
		response.Body = msgresp
	}
}

func (h *Hub) responseMessage(from wire.Addr, message *wire.Message) {
	if from.Type() == wire.AddrClient {
		h.clientPeers[from].PushMessage(message, nil)
	} else if from.Type() == wire.AddrServer {
		h.serverPeers[from].PushMessage(message, nil)
	}
}

func (h *Hub) sendToDomain(dest wire.Addr, message *wire.Message) {
	for addr, cpeer := range h.clientPeers {
		if addr.Domain() == dest.Domain() {
			cpeer.PushMessage(message, nil)
		}
	}
}

// broadcast message to all server
func (h *Hub) broadcast(message *wire.Message) {
	for _, speer := range h.serverPeers {
		speer.PushMessage(message, nil)
	}
}

func saveMessagesToDb(messageStore database.MessageStore, bufs []*bytes.Buffer) error {
	chatmsgs := make([]*database.ChatMsg, 0)
	groupmsgs := make([]*database.GroupMsg, 0)
	for _, buf := range bufs {
		packet := new(wire.Message)
		if err := packet.Decode(buf); err != nil {
			fmt.Println(err)
			continue
		}
		header := packet.Header
		if header.Command != wire.MsgTypeChat {
			continue
		}
		body := packet.Body.(*wire.Msgchat)
		if header.Dest.Type() == wire.AddrClient {
			dbmsg := &database.ChatMsg{
				FromDomain: header.Source.Domain(),
				ToDomain:   header.Dest.Domain(),
				From:       header.Source.Address(),
				To:         header.Dest.Address(),
				Type:       body.Type,
				Text:       body.Text,
				Extra:      body.Extra,
				CreateAt:   time.Now(),
			}
			chatmsgs = append(chatmsgs, dbmsg)
		} else if header.Dest.Type() == wire.AddrGroup {
			dbmsg := &database.GroupMsg{
				FromDomain: header.Source.Domain(),
				ToDomain:   header.Dest.Domain(),
				From:       header.Source.Address(),
				To:         header.Dest.Address(),
				Type:       body.Type,
				Text:       body.Text,
				Extra:      body.Extra,
				CreateAt:   time.Now(),
			}
			groupmsgs = append(groupmsgs, dbmsg)
		}
	}
	if len(chatmsgs) > 0 {
		err := messageStore.SaveChatMsg(chatmsgs)
		if err != nil {
			return err
		}
	}
	if len(groupmsgs) > 0 {
		err := messageStore.SaveGroupMsg(groupmsgs)
		if err != nil {
			return err
		}
	}

	// log.Printf("save messages : %v ", len(messages))
	return nil
}

// Close close hub
func (h *Hub) Close() {
	h.clean()

	h.quit <- struct{}{}
}

// clean clean hub
func (h *Hub) clean() {

	for _, speer := range h.serverPeers {
		speer.Close()
	}

	for _, cpeer := range h.clientPeers {
		cpeer.Close()
	}

	time.Sleep(time.Second)
}
