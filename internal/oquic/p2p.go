package oquic

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/pion/stun"
)

type progressTracker struct {
	Total       uint64
	Transferred uint64
	LastUpdate  time.Time
	LastBytes   uint64
}

func (pt *progressTracker) Write(p []byte) (int, error) {
	n := len(p)
	pt.Transferred += uint64(n)

	now := time.Now()
	elapsed := now.Sub(pt.LastUpdate)

	if elapsed >= 500*time.Millisecond {
		speed := float64(pt.Transferred-pt.LastBytes) / elapsed.Seconds()
		percent := float64(pt.Transferred) / float64(pt.Total) * 100

		fmt.Printf("\rProgress: %5.2f%% | Speed: %12s", percent, formatSpeed(speed))

		pt.LastUpdate = now
		pt.LastBytes = pt.Transferred
	}
	return n, nil
}

func formatSpeed(bytesPerSec float64) string {
	units := []string{"B/s", "KB/s", "MB/s", "GB/s"}
	i := 0
	for bytesPerSec >= 1024 && i < len(units)-1 {
		bytesPerSec /= 1024
		i++
	}
	return fmt.Sprintf("%.2f %s", bytesPerSec, units[i])
}

type PeerNetClient struct {
	udpConn *net.UDPConn

	token          uint64
	connectedToken uint64

	othersIP *net.UDPAddr
}

func NewPeerNetClient() (*PeerNetClient, error) {
	locAddr, _ := net.ResolveUDPAddr("udp", ":0")
	conn, err := net.ListenUDP("udp", locAddr)
	if err != nil {
		return nil, err
	}

	return &PeerNetClient{
		token:          0,
		connectedToken: 0,
		udpConn:        conn,
	}, nil
}

func (c *PeerNetClient) GetPublicAddr(stunServer string) (*net.UDPAddr, error) {
	serverAddr, err := net.ResolveUDPAddr("udp", stunServer)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve STUN server: %w", err)
	}

	req := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	if _, err := c.udpConn.WriteToUDP(req.Raw, serverAddr); err != nil {
		return nil, fmt.Errorf("failed to send STUN request: %w", err)
	}

	c.udpConn.SetReadDeadline(time.Now().Add(3 * time.Second))
	defer c.udpConn.SetReadDeadline(time.Time{})

	buf := make([]byte, 1500)
	n, addr, err := c.udpConn.ReadFrom(buf)
	if err != nil {
		return nil, fmt.Errorf("failed to read STUN response: %w", err)
	}

	uAddr, ok := addr.(*net.UDPAddr)
	if !ok || !uAddr.IP.Equal(serverAddr.IP) || uAddr.Port != serverAddr.Port {
		return nil, fmt.Errorf("got STUN response from unexpected address: %s", addr)
	}

	msg := new(stun.Message)
	msg.Raw = buf[:n]
	if err := msg.Decode(); err != nil {
		return nil, fmt.Errorf("failed to decode STUN message: %w", err)
	}

	var xorAddr stun.XORMappedAddress
	if err := xorAddr.GetFrom(msg); err != nil {
		return nil, fmt.Errorf("failed to get XORMappedAddress (maybe STUN error response?): %w", err)
	}

	return &net.UDPAddr{
		IP:   xorAddr.IP,
		Port: xorAddr.Port,
	}, nil
}

func (c *PeerNetClient) PunchNAT(othersAddr *net.UDPAddr) error {
	c.othersIP = othersAddr

	punchMsg := []byte{0x01, 0x03, 0x04, 0x01}
	ackMsg := []byte{0x01, 0x04, 0x03, 0x01}

	defer c.udpConn.SetReadDeadline(time.Time{})

	maxRetries := 25
	peerSeen := false

	for attempt := 1; attempt <= maxRetries; attempt++ {
		if !peerSeen {
			c.udpConn.WriteToUDP(punchMsg, c.othersIP)
		} else {
			c.udpConn.WriteToUDP(ackMsg, c.othersIP)
		}

		c.udpConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))

		buf := make([]byte, 1500)
		n, addr, err := c.udpConn.ReadFrom(buf)

		if err != nil {
			continue
		}

		uAddr, ok := addr.(*net.UDPAddr)
		if !ok || !uAddr.IP.Equal(c.othersIP.IP) || uAddr.Port != c.othersIP.Port {
			continue
		}

		if n == 4 && bytes.Equal(buf[:4], ackMsg) {
			for i := 0; i < 5; i++ {
				c.udpConn.WriteToUDP(ackMsg, c.othersIP)
				time.Sleep(10 * time.Millisecond)
			}
			return nil
		}

		if n == 4 && bytes.Equal(buf[:4], punchMsg) {
			peerSeen = true
			c.udpConn.WriteToUDP(ackMsg, c.othersIP)
			continue
		}

		if n > 4 {
			return nil
		}
	}

	return fmt.Errorf("NAT punch failed: peer is unreachable after %d attempts", maxRetries)
}

func (c *PeerNetClient) SendFile(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return err
	}

	client := NewQUICClient("PeerNet-v0.0.1", c.udpConn, context.Background())
	if err := client.Connect(c.othersIP); err != nil {
		return err
	}
	defer client.Close()

	if err := client.OpenStream(); err != nil {
		return err
	}
	s := client.Stream

	lbuf := make([]byte, 8)
	binary.BigEndian.PutUint64(lbuf, uint64(stat.Size()))
	if _, err := s.Write(lbuf); err != nil {
		return err
	}

	tracker := &progressTracker{
		Total:      uint64(stat.Size()),
		LastUpdate: time.Now(),
	}

	reader := io.TeeReader(f, tracker)

	fmt.Printf("Sending: %d bytes\n", stat.Size())
	written, err := io.Copy(s, reader)
	fmt.Println()

	if err != nil {
		return fmt.Errorf("streaming failed: %w", err)
	}

	if written != stat.Size() {
		return fmt.Errorf("partial write: %d of %d bytes", written, stat.Size())
	}

	buf := make([]byte, 8)
	if _, err := io.ReadFull(s, buf); err != nil {
		return err
	}
	clientRead := binary.BigEndian.Uint64(buf)
	if clientRead != uint64(stat.Size()) {
		return fmt.Errorf("receiver got only %d bytes", clientRead)
	}

	return nil
}

func (c *PeerNetClient) RecvFile(path string) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	serv, err := NewQUICServer("PeerNet-v0.0.1", c.udpConn, context.Background())
	if err != nil {
		return err
	}
	if err := serv.Start(); err != nil {
		return err
	}
	defer serv.Close()

	if err := serv.AcceptAndGetStream(); err != nil {
		return err
	}
	s := serv.Stream

	lbuf := make([]byte, 8)
	if _, err := io.ReadFull(s, lbuf); err != nil {
		return err
	}
	contentLen := binary.BigEndian.Uint64(lbuf)
	fmt.Printf("Receiving: %d bytes\n", contentLen)

	tracker := &progressTracker{
		Total:      contentLen,
		LastUpdate: time.Now(),
	}

	writer := io.MultiWriter(f, tracker)

	read, err := io.CopyN(writer, s, int64(contentLen))
	fmt.Println()

	if err != nil && err != io.EOF {
		return fmt.Errorf("streaming failed: %w", err)
	}

	if uint64(read) != contentLen {
		return fmt.Errorf("partial read: %d of %d bytes", read, contentLen)
	}

	binary.BigEndian.PutUint64(lbuf, contentLen)
	s.Write(lbuf)

	return nil
}
