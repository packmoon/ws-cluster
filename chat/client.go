// Copyright 2013 The Gorilla WebSocket Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/ws-cluster/wire"

	"github.com/gorilla/websocket"
	"github.com/ws-cluster/database"
	"github.com/ws-cluster/peer"
)

const (
	secret = "xxx123456"
)

func login(clientID, addr, secret string) (*peer.Peer, error) {
	nonce := fmt.Sprint(time.Now().UnixNano())
	h := md5.New()
	io.WriteString(h, clientID)
	io.WriteString(h, nonce)
	io.WriteString(h, secret)

	query := fmt.Sprintf("id=%v&nonce=%v&digest=%v", clientID, nonce, hex.EncodeToString(h.Sum(nil)))

	u := url.URL{Scheme: "ws", Host: addr, Path: "/client", RawQuery: query}
	log.Printf("connecting to %s", u.String())

	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Println("dial:", err)
		return nil, err
	}
	client := database.Client{
		ID: clientID,
	}
	OnMessage := func(message []byte) error {
		log.Println(message)
		return nil
	}
	OnDisconnect := func() error {
		return nil
	}
	peer := peer.NewPeer(fmt.Sprintf("C%v", client.ID), &peer.Config{
		Listeners: &peer.MessageListeners{
			OnMessage:    OnMessage,
			OnDisconnect: OnDisconnect,
		},
	})
	peer.SetConnection(conn)

	return peer, nil
}

func robot(clientID string, quit chan os.Signal) {
	peer, err := login(clientID, "localhost:8080", secret)
	if err != nil {
		log.Println(err)
		return
	}
	msg, _ := wire.MakeEmptyMessage(&wire.MessageHeader{ID: 1, Msgtype: wire.MsgTypeChat, Scope: wire.ScopeClient, To: "1"})
	chatMsg := msg.(*wire.Msgchat)
	chatMsg.From = clientID
	chatMsg.Type = 1
	chatMsg.Text = "hello, im robot"

	done := make(chan struct{})

	peer.SendMessage(chatMsg, done)
	<-done

	msg2, _ := wire.MakeEmptyMessage(&wire.MessageHeader{ID: 2, Msgtype: wire.MsgTypeChat, Scope: wire.ScopeGroup, To: "notify"})
	chatMsg2 := msg2.(*wire.Msgchat)
	chatMsg2.From = clientID
	chatMsg2.Type = 1
	chatMsg2.Text = "hello, group message"

	peer.SendMessage(chatMsg2, done)

	<-done
	// <-quit
	// peer.Peer.Close()

}

func main() {
	// listen sys.exit
	sc := make(chan os.Signal, 1)
	signal.Notify(sc, os.Interrupt)

	robot("system", sc)
}
