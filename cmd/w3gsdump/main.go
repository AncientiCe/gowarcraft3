// Author:  Niels A.D.
// Project: gowarcraft3 (https://github.com/nielsAD/gowarcraft3)
// License: Mozilla Public License, v2.0

// w3gsdump is a tool that decodes and dumps w3gs packets via pcap (on the wire or from a file).
package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/google/gopacket/pcap"
	"github.com/google/gopacket/tcpassembly"
	"github.com/google/gopacket/tcpassembly/tcpreader"
	"github.com/nielsAD/gowarcraft3/protocol"
	"github.com/nielsAD/gowarcraft3/protocol/w3gs"
)

var (
	fname   = flag.String("f", "", "Filename to read from")
	iface   = flag.String("i", "", "Interface to read packets from")
	promisc = flag.Bool("promisc", true, "Set promiscuous mode")
	snaplen = flag.Int("s", 65536, "Snap length (max number of bytes to read per packet")

	jsonout = flag.Bool("json", false, "Print machine readable format")
	bloblen = flag.Int("b", 128, "Max number of bytes to print per blob ")
)

var logOut = log.New(os.Stdout, "", log.Ltime)
var logErr = log.New(os.Stderr, "", log.Ltime)

func dumpPackets(layer string, netFlow, transFlow gopacket.Flow, r io.Reader) error {
	var dec = w3gs.NewDecoder(w3gs.Encoding{}, w3gs.NewFactoryCache(w3gs.DefaultFactory))

	var src = netFlow.Src().String() + ":" + transFlow.Src().String()
	var dst = netFlow.Dst().String() + ":" + transFlow.Dst().String()
	var prf = fmt.Sprintf("[%-3v] %21v->%-21v", layer, src, dst)

	for {
		var pkt w3gs.Packet

		var raw, _, err = dec.ReadRaw(r)
		if err == nil {
			pkt, _, err = dec.Deserialize(raw)
		}

		if err == io.EOF || err == w3gs.ErrNoProtocolSig {
			return err
		} else if err != nil {
			logErr.Printf("%v %-14v %v\n", prf, "ERROR", err)
			if len(raw) > 0 {
				logErr.Printf("Payload:\n%v", hex.Dump(raw))
			}

			if err == w3gs.ErrInvalidPacketSize || err == w3gs.ErrInvalidChecksum || err == w3gs.ErrUnexpectedConst {
				continue
			} else {
				return err
			}
		}

		// Truncate blobs
		switch p := pkt.(type) {
		case *w3gs.UnknownPacket:
			if len(p.Blob) > *bloblen {
				p.Blob = p.Blob[:*bloblen]
			}
		case *w3gs.GameAction:
			if len(p.Data) > *bloblen {
				p.Data = p.Data[:*bloblen]
			}
		case *w3gs.TimeSlot:
			var blobsize = 0
			for i := 0; i < len(p.Actions); i++ {
				if blobsize+len(p.Actions[i].Data) > *bloblen {
					p.Actions[i].Data = p.Actions[i].Data[:*bloblen-blobsize]
				}
				blobsize += len(p.Actions[i].Data)
				if blobsize >= *bloblen {
					p.Actions = p.Actions[:i+1]
					break
				}
			}
		case *w3gs.MapPart:
			p.Data = p.Data[:*bloblen]
		}

		var str = fmt.Sprintf("%+v", pkt)[1:]
		if *jsonout {
			if json, err := json.Marshal(pkt); err == nil {
				str = string(json)
			}
		}

		logOut.Printf("%v %-14v %v\n", prf, reflect.TypeOf(pkt).String()[6:], str)
	}
}

type streamFactory struct{}
type stream struct {
	netFlow   gopacket.Flow
	transFlow gopacket.Flow
	reader    tcpreader.ReaderStream
}

func (f *streamFactory) New(netFlow, transFlow gopacket.Flow) tcpassembly.Stream {
	var s = stream{
		netFlow:   netFlow,
		transFlow: transFlow,
		reader:    tcpreader.NewReaderStream(),
	}

	go s.run()

	return &s.reader
}

func (s *stream) run() {
	dumpPackets("TCP", s.netFlow, s.transFlow, &s.reader)
	io.Copy(ioutil.Discard, &s.reader)
}

func addHandle(h *pcap.Handle, c chan<- gopacket.Packet, wg *sync.WaitGroup) {
	if err := h.SetBPFFilter("(tcp and portrange 1000-65535) or (udp and port 6112)"); err != nil {
		logErr.Fatal("BPF filter error:", err)
	}

	var src = gopacket.NewPacketSource(h, h.LinkType())

	wg.Add(1)
	go func() {
		defer h.Close()
		defer wg.Done()

		for {
			p, err := src.NextPacket()
			if err == io.EOF {
				break
			} else if err != nil {
				logErr.Println("Sniffing error:", err)
			} else {
				c <- p
			}
		}
	}()
}

func main() {
	flag.Parse()
	if *jsonout {
		logOut.SetFlags(0)
	}

	var wg sync.WaitGroup
	var packets = make(chan gopacket.Packet)

	if *fname != "" {
		var handle, err = pcap.OpenOffline(*fname)
		if err != nil {
			logErr.Fatal("Could not open pcap file:", err)
		}
		addHandle(handle, packets, &wg)
	} else if *iface != "" {
		var handle, err = pcap.OpenLive(*iface, int32(*snaplen), *promisc, pcap.BlockForever)
		if err != nil {
			if devs, e := pcap.FindAllDevs(); e == nil {
				logErr.Print("Following interfaces are available:")
				for _, d := range devs {
					logErr.Printf("%v\t%v\n", d.Name, d.Description)
					for _, a := range d.Addresses {
						logErr.Printf("\t%v\n", a.IP)
					}
				}

				logErr.Fatalf("Could not create pcap handle: %v", err)
			}
		}
		addHandle(handle, packets, &wg)
	} else {
		var devs, err = pcap.FindAllDevs()
		if err != nil {
			logErr.Fatalf("Could not iterate interfaces: %v", err)
		}

		for _, d := range devs {
			if len(d.Addresses) == 0 {
				continue
			}

			var handle, err = pcap.OpenLive(d.Name, int32(*snaplen), *promisc, pcap.BlockForever)
			if err != nil {
				logErr.Fatalf("Could not create pcap handle: %v", err)
			}
			addHandle(handle, packets, &wg)
			logErr.Printf("Sniffing %v\n", d.Name)
		}
	}

	var asm = tcpassembly.NewAssembler(tcpassembly.NewStreamPool(&streamFactory{}))

	go func() {
		for packet := range packets {
			switch trans := packet.TransportLayer().(type) {
			case *layers.TCP:
				asm.Assemble(packet.NetworkLayer().NetworkFlow(), trans)
			case *layers.UDP:
				var buf = protocol.Buffer{Bytes: packet.ApplicationLayer().Payload()}
				dumpPackets("UDP", packet.NetworkLayer().NetworkFlow(), trans.TransportFlow(), &buf)
			}
		}
	}()

	wg.Wait()
	close(packets)
}
