// Author:  Niels A.D.
// Project: gowarcraft3 (https://github.com/nielsAD/gowarcraft3)
// License: Mozilla Public License, v2.0

// Package w3gs implements the game protocol for Warcraft III.
//
// Based on protocol documentation by https://bnetdocs.org/
//
// Each packet type is mapped to a struct type that implements the Packet
// interface. To deserialize from a binary stream, use DeserializePacket().
//
// This package tries to keep ammortized heap memory allocations to 0.
//
// General serialization format:
//
//    (UINT8)  Protocol signature (0xF7)
//    (UINT8)  Packet type ID
//    (UINT16) Packet size
//    [Packet Data]
//
package w3gs

import "github.com/nielsAD/gowarcraft3/protocol"

// Packet interface.
type Packet interface {
	Serialize(buf *protocol.Buffer, enc *Encoding) error
	Deserialize(buf *protocol.Buffer, enc *Encoding) error
}

// Encoding options for (de)serialization
type Encoding struct {
	GameVersion uint32
}
