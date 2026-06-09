package oquic

import (
	"context"
	"crypto/tls"
	"errors"
	"net"

	"github.com/quic-go/quic-go"
)

type QUICClient struct {
	ctx     context.Context
	tlsConf *tls.Config

	UDPConn *net.UDPConn
	Stream  *quic.Stream
	qconn   *quic.Conn

	remoteAddress net.Addr
	IsConnected   bool
}

func NewQUICClient(alpn string, udpConn *net.UDPConn, ctx context.Context) *QUICClient {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{alpn},
	}

	return &QUICClient{
		ctx:           ctx,
		tlsConf:       tlsConf,
		UDPConn:       udpConn,
		Stream:        nil,
		qconn:         nil,
		remoteAddress: nil,
		IsConnected:   false,
	}
}

func (c *QUICClient) Connect(remote net.Addr) error {
	c.remoteAddress = remote

	qconf := &quic.Config{
		MaxStreamReceiveWindow:     5 * 1024 * 1024,
		MaxConnectionReceiveWindow: 10 * 1024 * 1024,
	}

	conn, err := quic.Dial(c.ctx, c.UDPConn, remote, c.tlsConf, qconf)
	if err != nil {
		return err
	}

	c.qconn = conn
	c.IsConnected = true
	return nil
}

func (c *QUICClient) OpenStream() error {
	if !c.IsConnected {
		return errors.New("Client is not connected")
	}

	stream, err := c.qconn.OpenStreamSync(c.ctx)
	if err != nil {
		return err
	}

	c.Stream = stream
	return nil
}

func (c *QUICClient) Close() {
	if c.Stream != nil {
		c.Stream.Close()
	}

	if c.qconn != nil {
		c.qconn.CloseWithError(0, "closed")
	}
}
