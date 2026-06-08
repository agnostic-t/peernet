package oquic

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"math/big"
	"net"

	"github.com/quic-go/quic-go"
)

type QUICServer struct {
	ctx     context.Context
	tlsCert tls.Certificate
	tlsConf *tls.Config

	UDPConn *net.UDPConn
	Stream  *quic.Stream
	qconn   *quic.Conn

	listener    *quic.Listener
	IsConnected bool
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		DNSNames:     []string{"localhost"},
	}
	certDER, _ := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	return tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  priv,
	}, nil
}

func NewQUICServer(alpn string, udpConn *net.UDPConn, ctx context.Context) (*QUICServer, error) {
	cert, err := generateSelfSignedCert()
	if err != nil {
		return nil, err
	}

	conf := &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{alpn},
	}

	return &QUICServer{
		tlsCert:     cert,
		tlsConf:     conf,
		UDPConn:     udpConn,
		Stream:      nil,
		qconn:       nil,
		IsConnected: false,
		ctx:         ctx,
		listener:    nil,
	}, nil
}

func (s *QUICServer) Start() error {
	listener, err := quic.Listen(s.UDPConn, s.tlsConf, nil)
	if err != nil {
		return err
	}

	s.listener = listener
	return nil
}

func (s *QUICServer) AcceptAndGetStream() error {
	conn, err := s.listener.Accept(s.ctx)
	if err != nil {
		return err
	}

	s.qconn = conn
	stream, err := conn.AcceptStream(s.ctx)
	if err != nil {
		return err
	}

	s.Stream = stream
	return nil
}

func (s *QUICServer) Close() {
	if s.listener != nil {
		s.listener.Close()
	}

	if s.Stream != nil {
		s.Stream.Close()
	}
}
