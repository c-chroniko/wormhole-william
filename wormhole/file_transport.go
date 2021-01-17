package wormhole

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/psanford/wormhole-william/internal/crypto"
	"golang.org/x/crypto/hkdf"
	"golang.org/x/crypto/nacl/secretbox"
	ws "nhooyr.io/websocket"
)

type fileTransportAck struct {
	Ack    string `json:"ack"`
	SHA256 string `json:"sha256"`
}

type TransferType int

const (
	TransferFile TransferType = iota + 1
	TransferDirectory
	TransferText
)

func (tt TransferType) String() string {
	switch tt {
	case TransferFile:
		return "TransferFile"
	case TransferDirectory:
		return "TransferDirectory"
	case TransferText:
		return "TransferText"
	default:
		return fmt.Sprintf("TransferTypeUnknown<%d>", tt)
	}
}

type transportCryptor struct {
	conn           net.Conn
	prefixBuf      []byte
	nextReadNonce  *big.Int
	nextWriteNonce uint64
	err            error
	readKey        [32]byte
	writeKey       [32]byte
}

func newTransportCryptor(c net.Conn, transitKey []byte, readPurpose, writePurpose string) *transportCryptor {
	r := hkdf.New(sha256.New, transitKey, nil, []byte(readPurpose))
	var readKey [32]byte
	_, err := io.ReadFull(r, readKey[:])
	if err != nil {
		panic(err)
	}

	r = hkdf.New(sha256.New, transitKey, nil, []byte(writePurpose))
	var writeKey [32]byte
	_, err = io.ReadFull(r, writeKey[:])
	if err != nil {
		panic(err)
	}

	return &transportCryptor{
		conn:          c,
		prefixBuf:     make([]byte, 4+crypto.NonceSize),
		nextReadNonce: big.NewInt(0),
		readKey:       readKey,
		writeKey:      writeKey,
	}
}
func (d *transportCryptor) Close() error {
	return d.conn.Close()
}

func (d *transportCryptor) readRecord() ([]byte, error) {
	if d.err != nil {
		return nil, d.err
	}
	_, err := io.ReadFull(d.conn, d.prefixBuf)
	if err != nil {
		d.err = err
		return nil, d.err
	}

	l := binary.BigEndian.Uint32(d.prefixBuf[:4])
	var nonce [24]byte
	copy(nonce[:], d.prefixBuf[4:])

	var bigNonce big.Int
	bigNonce.SetBytes(nonce[:])

	if bigNonce.Cmp(d.nextReadNonce) != 0 {
		d.err = errors.New("received out-of-order record")
		return nil, d.err
	}

	d.nextReadNonce.Add(d.nextReadNonce, big.NewInt(1))

	sealedMsg := make([]byte, l-crypto.NonceSize)
	_, err = io.ReadFull(d.conn, sealedMsg)
	if err != nil {
		d.err = err
		return nil, d.err
	}

	out, ok := secretbox.Open(nil, sealedMsg, &nonce, &d.readKey)
	if !ok {
		d.err = errDecryptFailed
		return nil, d.err
	}

	return out, nil
}

func (d *transportCryptor) writeRecord(msg []byte) error {
	var nonce [crypto.NonceSize]byte

	if d.nextWriteNonce == math.MaxUint64 {
		panic("Nonce exhaustion")
	}

	binary.BigEndian.PutUint64(nonce[crypto.NonceSize-8:], d.nextWriteNonce)
	d.nextWriteNonce++

	sealedMsg := secretbox.Seal(nil, msg, &nonce, &d.writeKey)

	nonceAndSealedMsg := append(nonce[:], sealedMsg...)

	// we do an explit cast to int64 to avoid compilation failures
	// for 32bit systems.
	nonceAndSealedMsgSize := int64(len(nonceAndSealedMsg))

	if nonceAndSealedMsgSize >= math.MaxUint32 {
		panic(fmt.Sprintf("writeRecord too large: %d", len(nonceAndSealedMsg)))
	}

	l := make([]byte, 4)
	binary.BigEndian.PutUint32(l, uint32(len(nonceAndSealedMsg)))

	lenNonceAndSealedMsg := append(l, nonceAndSealedMsg...)

	_, err := d.conn.Write(lenNonceAndSealedMsg)
	return err
}

func newFileTransport(transitKey []byte, appID, relayAddr string) *fileTransport {
	return &fileTransport{
		transitKey: transitKey,
		appID:      appID,
		relayAddr:  relayAddr,
	}
}

type fileTransport struct {
	listener   net.Listener
	relayConn  net.Conn
	relayAddr  string
	transitKey []byte
	appID      string
}

func (t *fileTransport) connectViaRelay(otherTransit *transitMsg) (net.Conn, error) {
	fmt.Println("connectViaRelay")
	cancelFuncs := make(map[string]func())

	successChan := make(chan net.Conn)
	failChan := make(chan string)

	var count int

	for _, outerHint := range otherTransit.HintsV1 {
		fmt.Println("if outerHint.type == \"relay-v1\"...")
		if outerHint.Type == "relay-v1" {
			fmt.Println("true")
			for _, innerHint := range outerHint.Hints {
				fmt.Println("if innerHint.type == \"direct-tcp-v1\"")
				if innerHint.Type == "direct-tcp-v1" {
					fmt.Println("true")
					count++
					ctx, cancel := context.WithCancel(context.Background())
					addr := net.JoinHostPort(innerHint.Hostname, strconv.Itoa(innerHint.Port))

					cancelFuncs[addr] = cancel

					fmt.Println("go t.connectToRelay")
					go t.connectToRelay(ctx, addr, successChan, failChan)
				} else {
					fmt.Println("false")
				}
			}
		} else {
			fmt.Println("false")
		}
	}

	var conn net.Conn

	connectTimeout := time.After(5 * time.Second)

	for i := 0; i < count; i++ {
		select {
		case <-failChan:
		case conn = <-successChan:
		case <-connectTimeout:
			for _, cancel := range cancelFuncs {
				cancel()
			}
		}
	}

	return conn, nil
}

func (t *fileTransport) connectDirect(otherTransit *transitMsg) (net.Conn, error) {
	cancelFuncs := make(map[string]func())

	successChan := make(chan net.Conn)
	failChan := make(chan string)

	var count int

	for _, hint := range otherTransit.HintsV1 {
		if hint.Type == "direct-tcp-v1" {
			count++
			ctx, cancel := context.WithCancel(context.Background())
			addr := net.JoinHostPort(hint.Hostname, strconv.Itoa(hint.Port))

			cancelFuncs[addr] = cancel

			go t.connectToSingleHost(ctx, addr, successChan, failChan)
		}
	}

	var conn net.Conn

	connectTimeout := time.After(5 * time.Second)

	for i := 0; i < count; i++ {
		select {
		case <-failChan:
		case conn = <-successChan:
		case <-connectTimeout:
			for _, cancel := range cancelFuncs {
				cancel()
			}
		}
	}

	return conn, nil
}

func (t *fileTransport) connectToRelay(ctx context.Context, addr string, successChan chan net.Conn, failChan chan string) {
	fmt.Println("t *fileTransport#connectToRelay")

	// TODO: refactor!
	hostParts := strings.SplitN(t.relayAddr, "://", 2)
	fmt.Printf("hostParts: %v\n", hostParts)
	var (
		d    net.Dialer
		conn net.Conn
		err  error
	)
	if len(hostParts) == 2 {
		fmt.Println("hostParts == 2")
		proto := hostParts[0]
		if proto == "ws" || proto == "wss" {
			fmt.Println("proto == ws/wss")
			wsConn, _, err := ws.Dial(ctx, t.relayAddr, nil)
			if err != nil {
				failChan <- addr
				return
			}
			conn = ws.NetConn(ctx, wsConn, ws.MessageBinary)
			fmt.Printf("conn = ws.NetConn: %v\n", conn)
		}
	} else {
		//fmt.Println("conn = net.Dial tcp")
		//conn, err = net.Dial("tcp", t.relayAddr)
		//if err != nil {
		//	failChan <- addr
		//	return
		//}

		fmt.Println("conn = d.DialContext tcp")
		conn, err = d.DialContext(ctx, "tcp", addr)
		if err != nil {
			failChan <- addr
			fmt.Println("error in conn = d.DialContext tcp")
			return
		}
	}


	fmt.Println("conn.Write(t.relayHandhskeHeader())")
	fmt.Printf("conn: %v\n", conn)
	_, err = conn.Write(t.relayHandshakeHeader())
	if err != nil {
		fmt.Println("error in conn.Write(t.relayHandhskeHeader())")
		failChan <- addr
		return
	}
	gotOk := make([]byte, 2)
	fmt.Println("io.ReadFull(conn, gotOk)")
	fmt.Printf("conn: %v\n", conn)
	n, err := io.ReadFull(conn, gotOk)
	if err != nil {
		fmt.Printf("error in io.ReadFull(conn, gotOk)| n: %d; err: %s\n", n, err)
		conn.Close()
		failChan <- addr
		return
	}

	fmt.Printf("!bytes.Equal(gotOk, ...): %s | %v\n", string(gotOk), gotOk)
	if !bytes.Equal(gotOk, []byte("ok")) {
		conn.Close()
		failChan <- addr
		fmt.Printf("error in !bytes.Equal(gotOk, ...)| gotOk: %s\n", gotOk)
		return
	}

	fmt.Println("t.directRecvHAndshake")
	t.directRecvHandshake(ctx, addr, conn, successChan, failChan)
}

func (t *fileTransport) connectToSingleHost(ctx context.Context, addr string, successChan chan net.Conn, failChan chan string) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)

	if err != nil {
		failChan <- addr
		return
	}

	t.directRecvHandshake(ctx, addr, conn, successChan, failChan)
}

func (t *fileTransport) directRecvHandshake(ctx context.Context, addr string, conn net.Conn, successChan chan net.Conn, failChan chan string) {
	fmt.Println("hello from directRecvHandshake")
	expectHeader := t.senderHandshakeHeader()

	gotHeader := make([]byte, len(expectHeader))

	fmt.Println("directRecvHandshake->io.ReadFull")
	n, err := io.ReadFull(conn, gotHeader)
	fmt.Printf("n: %d; gotHeader: %s; err: %s\n", n, gotHeader, err)
	if err != nil {
		fmt.Printf("io.ReadFull err: %s\n", err)
		conn.Close()
		failChan <- addr
		return
	}

	fmt.Println("subtle.ConstantTimeCompare")
	if subtle.ConstantTimeCompare(gotHeader, expectHeader) != 1 {
		fmt.Println("subtle.ConstantTimeCompare err")
		conn.Close()
		failChan <- addr
		return
	}

	fmt.Println("con.Write(t.receiverHAndshakeHeader")
	_, err = conn.Write(t.receiverHandshakeHeader())
	if err != nil {
		fmt.Println("con.Write(t.receiverHAndshakeHeader err")
		conn.Close()
		failChan <- addr
		return
	}

	gotGo := make([]byte, 2)
	fmt.Println("io.ReadFull 2")
	_, err = io.ReadFull(conn, gotGo)
	fmt.Println("io.ReadFull 2 err")
	if err != nil {
		conn.Close()
		failChan <- addr
		return
	}

	if !bytes.Equal(gotGo, []byte("go")) {
		conn.Close()
		failChan <- addr
		return
	}

	successChan <- conn
}

func (t *fileTransport) makeTransitMsg() (*transitMsg, error) {
	msg := transitMsg{
		AbilitiesV1: []transitAbility{
			{
				Type: "direct-tcp-v1",
			},
			{
				Type: "relay-v1",
			},
		},
		// make a slice so this jsons to [] and not null
		HintsV1: make([]transitHintsV1, 0),
	}

	if t.listener != nil {
		fmt.Println("t.listener != nil!!!")
		_, portStr, err := net.SplitHostPort(t.listener.Addr().String())
		if err != nil {
			return nil, err
		}

		port, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("port isn't an integer? %s", portStr)
		}

		addrs := nonLocalhostAddresses()

		for _, addr := range addrs {
			msg.HintsV1 = append(msg.HintsV1, transitHintsV1{
				Type:     "direct-tcp-v1",
				Priority: 0.0,
				Hostname: addr,
				Port:     port,
			})
		}
	}

	if t.relayConn != nil {
		fmt.Println("t.relayConn != nil!!!")
		var (
			portStr, relayHost string
			err                error
		)
		switch {
		case strings.HasPrefix(t.relayAddr, "ws://"):
			relayHost, portStr, err = net.SplitHostPort(t.relayAddr[5:])
		case strings.HasPrefix(t.relayAddr, "wss://"):
			relayHost, portStr, err = net.SplitHostPort(t.relayAddr[6:])
		default:
			relayHost, portStr, err = net.SplitHostPort(t.relayAddr)
		}
		if err != nil {
			return nil, err
		}

		relayPort, err := strconv.Atoi(portStr)
		if err != nil {
			return nil, fmt.Errorf("port isn't an integer? %s", portStr)
		}

		msg.HintsV1 = append(msg.HintsV1, transitHintsV1{
			Type: "relay-v1",
			Hints: []transitHintsV1Hint{
				{
					Type:     "direct-tcp-v1",
					Priority: 2.0,
					Hostname: relayHost,
					Port:     relayPort,
				},
			},
		})
	}

	return &msg, nil
}

func (t *fileTransport) senderHandshakeHeader() []byte {
	purpose := "transit_sender"

	r := hkdf.New(sha256.New, t.transitKey, nil, []byte(purpose))
	out := make([]byte, 32)

	_, err := io.ReadFull(r, out)
	if err != nil {
		panic(err)
	}

	return []byte(fmt.Sprintf("transit sender %x ready\n", out))
}

func (t *fileTransport) receiverHandshakeHeader() []byte {
	purpose := "transit_receiver"

	r := hkdf.New(sha256.New, t.transitKey, nil, []byte(purpose))
	out := make([]byte, 32)

	_, err := io.ReadFull(r, out)
	if err != nil {
		panic(err)
	}

	return []byte(fmt.Sprintf("transit receiver %x ready\n", out))
}

func (t *fileTransport) relayHandshakeHeader() []byte {
	purpose := "transit_relay_token"

	r := hkdf.New(sha256.New, t.transitKey, nil, []byte(purpose))
	out := make([]byte, 32)

	_, err := io.ReadFull(r, out)
	if err != nil {
		panic(err)
	}

	sideID := crypto.RandHex(8)

	return []byte(fmt.Sprintf("please relay %x for side %s\n", out, sideID))
}

// Test option to disable local listeners
var testDisableLocalListener bool

func (t *fileTransport) listen() error {
	if testDisableLocalListener {
		return nil
	}

	l, err := net.Listen("tcp", ":0")
	if err != nil {
		return err
	}

	t.listener = l
	return nil
}

func (t *fileTransport) listenRelay() error {
	fmt.Println("hello from listenRelay")

	// TODO: consider if/how contexts are being used and be consistent and/or correct.
	ctx := context.Background()
	if t.relayAddr == "" {
		return nil
	}

	hostParts := strings.SplitN(t.relayAddr, "://", 2)
	fmt.Printf("hostParts: %v\n", hostParts)
	var (
		conn net.Conn
		err  error
	)
	if len(hostParts) == 2 {
		fmt.Println("hostParts == 2")
		proto := hostParts[0]
		if proto == "ws" || proto == "wss" {
			fmt.Println("proto == ws/wss")
			wsConn, _, err := ws.Dial(ctx, t.relayAddr, nil)
			if err != nil {
				return err
			}
			fmt.Println("conn = ws.NetConn")
			conn = ws.NetConn(ctx, wsConn, ws.MessageBinary)
		}
	} else {
		fmt.Println("conn = net.Dial tcp")
		conn, err = net.Dial("tcp", t.relayAddr)
		if err != nil {
			return err
		}
	}

	fmt.Println("connWrite")
	_, err = conn.Write(t.relayHandshakeHeader())
	if err != nil {
		conn.Close()
		return err
	}

	fmt.Println("t.relayConn = conn")
	t.relayConn = conn
	return nil
}

func (t *fileTransport) waitForRelayPeer(conn net.Conn, cancelCh chan struct{}) error {
	fmt.Println("hello from waitForRelayPeer")
	okCh := make(chan struct{})
	go func() {
		select {
		case <-cancelCh:
			fmt.Println("<-cancelCh")
			conn.Close()
		case <-okCh:
			fmt.Println("<-okCh")
		}
	}()

	defer close(okCh)

	gotOk := make([]byte, 2)
	_, err := io.ReadFull(conn, gotOk)
	if err != nil {
		fmt.Printf("waitForRelayPeer->io.ReadFull err: %s\n", err)
		conn.Close()
		return err
	}

	fmt.Printf("gotOk: %v", gotOk)
	if !bytes.Equal(gotOk, []byte("ok")) {
		conn.Close()
		return errors.New("got non ok status from relay server")
	}

	return nil
}

func (t *fileTransport) acceptConnection(ctx context.Context) (net.Conn, error) {
	readyCh := make(chan net.Conn)
	cancelCh := make(chan struct{})
	acceptErrCh := make(chan error, 1)

	if t.relayConn != nil {
		go func() {
			waitErr := t.waitForRelayPeer(t.relayConn, cancelCh)
			if waitErr != nil {
				return
			}
			t.handleIncomingConnection(t.relayConn, readyCh, cancelCh)
		}()
	}

	if t.listener != nil {
		defer t.listener.Close()

		go func() {
			for {
				conn, err := t.listener.Accept()
				if err == io.EOF {
					break
				} else if err != nil {
					acceptErrCh <- err
					break
				}

				go t.handleIncomingConnection(conn, readyCh, cancelCh)
			}
		}()
	}

	select {
	case <-ctx.Done():
		close(cancelCh)
		return nil, ctx.Err()
	case acceptErr := <-acceptErrCh:
		close(cancelCh)
		return nil, acceptErr
	case conn := <-readyCh:
		close(cancelCh)
		_, err := conn.Write([]byte("go\n"))
		if err != nil {
			return nil, err
		}

		return conn, nil
	}
}

func (t *fileTransport) handleIncomingConnection(conn net.Conn, readyCh chan<- net.Conn, cancelCh chan struct{}) {
	okCh := make(chan struct{})

	go func() {
		select {
		case <-cancelCh:
			conn.Close()
		case <-okCh:
		}
	}()

	_, err := conn.Write(t.senderHandshakeHeader())
	if err != nil {
		conn.Close()
		close(okCh)
		return
	}

	expectHeader := t.receiverHandshakeHeader()

	gotHeader := make([]byte, len(expectHeader))

	_, err = io.ReadFull(conn, gotHeader)
	if err != nil {
		conn.Close()
		close(okCh)
		return
	}

	if subtle.ConstantTimeCompare(gotHeader, expectHeader) != 1 {
		conn.Close()
		close(okCh)
		return
	}

	select {
	case okCh <- struct{}{}:
	case <-cancelCh:
	}

	select {
	case <-cancelCh:
		// One of the other connections won, shut this one down
		conn.Write([]byte("nevermind\n"))
		conn.Close()
	case readyCh <- conn:
	}
}

func nonLocalhostAddresses() []string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil
	}

	var outAddrs []string

	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				outAddrs = append(outAddrs, ipnet.IP.String())
			}
		}
	}

	return outAddrs
}
