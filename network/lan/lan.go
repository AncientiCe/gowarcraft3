// Author:  Niels A.D.
// Project: gowarcraft3 (https://github.com/nielsAD/gowarcraft3)
// License: Mozilla Public License, v2.0

// Package lan implements a mocked Warcraft III LAN client that can be used to discover local games.
package lan

import (
	"context"
	"net"
	"sync"

	"github.com/nielsAD/gowarcraft3/network"
	"github.com/nielsAD/gowarcraft3/protocol"
	"github.com/nielsAD/gowarcraft3/protocol/w3gs"
)

// FindGame returns the an arbitrary game hosted in LAN
func FindGame(ctx context.Context, gv w3gs.GameVersion) (addr string, hostCounter uint32, entryKey uint32, err error) {
	var g GameList
	g, err = NewGameList(gv)
	if err != nil {
		return
	}

	var stop = make(chan error)
	g.On(Update{}, func(ev *network.Event) {
		for k, v := range g.Games() {
			addr = k
			hostCounter = v.HostCounter
			entryKey = v.EntryKey
			stop <- nil
			return
		}
	})
	g.On(&network.AsyncError{}, func(ev *network.Event) {
		var err = ev.Arg.(*network.AsyncError)
		stop <- err
	})

	go func() {
		var err = g.Run()
		stop <- err
	}()

	select {
	case <-ctx.Done():
		err = ctx.Err()
	case e := <-stop:
		err = e
	}

	g.Close()
	return
}

var bcmut sync.Mutex
var bccon *net.UDPConn
var bcbuf protocol.Buffer

// Broadcast hosted game information to LAN
// Safe for concurrent invocation
func Broadcast(game *w3gs.GameInfo) (err error) {
	bcmut.Lock()

	if bccon == nil {
		bccon, err = net.ListenUDP("udp4", &net.UDPAddr{})
	}

	if err == nil {
		bcbuf.Truncate()
		if err = game.Serialize(&bcbuf, &w3gs.Encoding{}); err == nil {
			_, err = bccon.WriteTo(bcbuf.Bytes, &network.W3GSBroadcastAddr)
		}
	}

	bcmut.Unlock()
	return err
}
