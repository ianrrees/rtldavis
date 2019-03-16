/*
   rtldavis, an rtl-sdr receiver for Davis Instruments weather stations.
   Copyright (C) 2015  Douglas Hall

   This program is free software: you can redistribute it and/or modify
   it under the terms of the GNU General Public License as published by
   the Free Software Foundation, either version 3 of the License, or
   (at your option) any later version.

   This program is distributed in the hope that it will be useful,
   but WITHOUT ANY WARRANTY; without even the implied warranty of
   MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
   GNU General Public License for more details.

   You should have received a copy of the GNU General Public License
   along with this program.  If not, see <http://www.gnu.org/licenses/>.

   Modified by Luc Heijst - March 2019
   Added: EU-frequencies
   Removed: frequency correction
   Removed: parsing
   VERSION: 0.8
*/
package protocol

import (
	"fmt"
	"math"
	"math/rand"

	"github.com/lheijst/rtldavis/crc"
	"github.com/lheijst/rtldavis/dsp"
)

func NewPacketConfig(symbolLength int) (cfg dsp.PacketConfig) {
	return dsp.NewPacketConfig(
		19200,
		14,
		16,
		80,
		"1100101110001001",
	)
}

type Parser struct {
	dsp.Demodulator
	crc.CRC
	Cfg dsp.PacketConfig
	ChannelCount 	int
	channels		[]int
	hopIdx			int
	hopPattern		[]int
	reverseHopPattern []int
	freqError		int
}

func NewParser(symbolLength int, tf string) (p Parser) {
	p.Cfg = NewPacketConfig(symbolLength)
	p.Demodulator = dsp.NewDemodulator(&p.Cfg)
	p.CRC = crc.NewCRC("CCITT-16", 0, 0x1021, 0)

	if tf == "EU" {
		p.channels = []int{ 
// my-orig	868072000, 868192000, 868312000, 868432000, 868552000,
// ok		868073575, 868192150, 868312125, 868433150, 868553825,
			868078500, 868199000, 868318500, 868438500, 868560000,
		}
		p.ChannelCount = len(p.channels)

		p.hopIdx = rand.Intn(p.ChannelCount)
		p.hopPattern = []int{
			0, 2, 4, 1, 3,   
		}
		p.reverseHopPattern = []int{
			0, 2, 4, 1, 3,   
		}
	} else {
		p.channels = []int{
			902355835, 902857585, 903359336, 903861086, 904362837, 904864587,
			905366338, 905868088, 906369839, 906871589, 907373340, 907875090,
			908376841, 908878591, 909380342, 909882092, 910383843, 910885593,
			911387344, 911889094, 912390845, 912892595, 913394346, 913896096,
			914397847, 914899597, 915401347, 915903098, 916404848, 916906599,
			917408349, 917910100, 918411850, 918913601, 919415351, 919917102,
			920418852, 920920603, 921422353, 921924104, 922425854, 922927605,
			923429355, 923931106, 924432856, 924934607, 925436357, 925938108,
			926439858, 926941609, 927443359,
		}
		p.ChannelCount = len(p.channels)

		p.hopIdx = rand.Intn(p.ChannelCount)
		p.hopPattern = []int{
			0, 19, 41, 25, 8, 47, 32, 13, 36, 22, 3, 29, 44, 16, 5, 27, 38, 10,
			49, 21, 2, 30, 42, 14, 48, 7, 24, 34, 45, 1, 17, 39, 26, 9, 31, 50,
			37, 12, 20, 33, 4, 43, 28, 15, 35, 6, 40, 11, 23, 46, 18,
		}
		p.reverseHopPattern = []int{
			0, 19, 41, 25, 8, 47, 32, 13, 36, 22, 3, 29, 44, 16, 5, 27, 38, 10,
			49, 21, 2, 30, 42, 14, 48, 7, 24, 34, 45, 1, 17, 39, 26, 9, 31, 50,
			37, 12, 20, 33, 4, 43, 28, 15, 35, 6, 40, 11, 23, 46, 18,
		}
	}
	// create reverseHopPattern
	for i := 0; i < p.ChannelCount; i++ {
		p.reverseHopPattern[i] = (p.ChannelCount - p.hopPattern[i]) % p.ChannelCount
	}
	return
}

type Hop struct {
	ChannelIdx  int
	ChannelFreq int
	FreqError   int
}

func (h Hop) String() string {
	return fmt.Sprintf("{ChannelIdx:%d ChannelFreq:%d FreqError:%d}",
		h.ChannelIdx, h.ChannelFreq, h.FreqError,
	)
}

func (p *Parser) hop() (h Hop) {
	h.ChannelIdx = p.hopPattern[p.hopIdx]
	h.ChannelFreq = p.channels[h.ChannelIdx]
	h.FreqError = p.freqError
	return h
}

// Set the pattern index and return the new channel's parameters.
func (p *Parser) SetHop(n int) Hop {
	p.hopIdx = n % p.ChannelCount
	return p.hop()
}

// Find sequence-id with hop-id
func (p *Parser) HopToSeq(n int) int {
	return p.reverseHopPattern[n % p.ChannelCount]
}

// Find hop-id with sequence-id
func (p *Parser) SeqToHop(n int) int {
	return p.hopPattern[n % p.ChannelCount]
}

// Given a list of packets, check them for validity and ignore duplicates,
// return a list of parsed messages.
func (p *Parser) Parse(pkts []dsp.Packet) (msgs []Message) {
	seen := make(map[string]bool)

	for _, pkt := range pkts {
		// Bit order over-the-air is reversed.
		for idx, b := range pkt.Data {
			pkt.Data[idx] = SwapBitOrder(b)
		}
		// Keep track of duplicate packets.
		s := string(pkt.Data)
		if seen[s] {
			continue
		}
		seen[s] = true

		// If the checksum fails, bail.
		if p.Checksum(pkt.Data[2:]) != 0 {
			continue
		}

		// Look at the packet's tail to determine frequency error between
		// transmitter and receiver.
		lower := pkt.Idx + 8*p.Cfg.SymbolLength
		upper := pkt.Idx + 24*p.Cfg.SymbolLength
		tail := p.Demodulator.Discriminated[lower:upper]
		var mean float64
		for _, sample := range tail {
			mean += sample
		}
		mean /= float64(len(tail))

		// The tail is a series of zero symbols. The driminator's output is
		// measured in radians.
		p.freqError = -int(9600 + (mean*float64(p.Cfg.SampleRate))/(2*math.Pi))
		msgs = append(msgs, NewMessage(pkt))
	}
	return
}

type Message struct {
	dsp.Packet
	ID     byte
}

func NewMessage(pkt dsp.Packet) (m Message) {
	m.Idx = pkt.Idx
	m.Data = make([]byte, len(pkt.Data)-2)
	copy(m.Data, pkt.Data[2:])
	m.ID = m.Data[0] & 0x7
	return m
}

func (m Message) String() string {
	return fmt.Sprintf("{ID:%d}", m.ID)
}

func SwapBitOrder(b byte) byte {
	b = ((b & 0xF0) >> 4) | ((b & 0x0F) << 4)
	b = ((b & 0xCC) >> 2) | ((b & 0x33) << 2)
	b = ((b & 0xAA) >> 1) | ((b & 0x55) << 1)
	return b
}
