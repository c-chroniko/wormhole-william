package wormhole

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"

	"github.com/psanford/wormhole-william/internal/crypto"
	"github.com/psanford/wormhole-william/rendezvous"
)

// Receive receives a message sent by a wormhole client.
//
// It returns an IncomingMessage with metadata about the payload being sent.
// To read the contents of the message call IncomingMessage.Read().
func (c *Client) Receive(ctx context.Context, code string, opts ...TransferOption) (fr *IncomingMessage, returnErr error) {
	sideID := crypto.RandSideID()
	appID := c.appID()
	rc := rendezvous.NewClient(c.url(), sideID, appID)

	_, err := rc.Connect(ctx)
	if err != nil {
		return nil, err
	}
	nameplate, err := nameplateFromCode(code)
	if err != nil {
		return nil, err
	}

	err = rc.AttachMailbox(ctx, nameplate)
	if err != nil {
		return nil, err
	}

	clientProto := newClientProtocol(ctx, rc, sideID, appID)

	err = clientProto.WritePake(ctx, code)
	if err != nil {
		return nil, err
	}

	err = clientProto.ReadPake(ctx)
	if err != nil {
		return nil, err
	}

	err = clientProto.WriteVersion(ctx)
	if err != nil {
		return nil, err
	}

	_, err = clientProto.ReadVersion()
	if err != nil {
		return nil, err
	}

	if c.VerifierOk != nil {
		verifier, err := clientProto.Verifier()
		if err != nil {
			return nil, err
		}

		if ok := c.VerifierOk(hex.EncodeToString(verifier)); !ok {
			errMsg := "sender rejected verification check, abandoned transfer"
			writeErr := clientProto.WriteAppData(ctx, &genericMessage{
				Error: &errMsg,
			})
			if writeErr != nil {
				return nil, writeErr
			}

			return nil, errors.New(errMsg)
		}
	}

	collector, err := clientProto.Collect(collectOffer, collectTransit)
	if err != nil {
		return nil, err
	}
	defer collector.close()

	fr = &IncomingMessage{}
	for _, opt := range opts {
		err := opt.setOption(&fr.options)
		if err != nil {
			return nil, err
		}
	}

	// TODO: factor out -- only depends on `clientProto`.
	reject := func() error {
		defer func() {
			mood := rendezvous.Errory
			if returnErr == nil {
				mood = rendezvous.Happy
			} else if returnErr == errDecryptFailed {
				mood = rendezvous.Scary
			}
			clientProto.rc.Close(ctx, mood)
		}()

		var errStr = "transfer rejected"
		answer := &genericMessage{
			Error: &errStr,
		}

		err = clientProto.WriteAppData(ctx, answer)
		if err != nil {
			return err
		}

		return nil
	}
	fr.rejectTransfer = reject

	var offer OfferMsg
	err = collector.waitFor(&offer)
	if err != nil {
		return nil, err
	}

	switch {
	case offer.Message != nil:
		answer := genericMessage{
			Answer: &answerMsg{
				MessageAck: "ok",
			},
		}

		err = clientProto.WriteAppData(ctx, &answer)
		if err != nil {
			return nil, err
		}

		rc.Close(ctx, rendezvous.Happy)

		fr.Type = TransferText
		fr.textReader = bytes.NewReader([]byte(*offer.Message))
		return fr, nil
	case offer.File != nil:
		fr.Type = TransferFile
		fr.Name = offer.File.FileName
		fr.TransferBytes = int(offer.File.FileSize)
		fr.TransferBytes64 = offer.File.FileSize
		fr.UncompressedBytes = int(offer.File.FileSize)
		fr.UncompressedBytes64 = offer.File.FileSize
		fr.FileCount = 1
		fr.ctx = ctx
	case offer.Directory != nil:
		fr.Type = TransferDirectory
		fr.Name = offer.Directory.Dirname
		fr.TransferBytes = int(offer.Directory.ZipSize)
		fr.TransferBytes64 = offer.Directory.ZipSize
		fr.UncompressedBytes = int(offer.Directory.NumBytes)
		fr.UncompressedBytes64 = offer.Directory.NumBytes
		fr.FileCount = int(offer.Directory.NumFiles)
		fr.ctx = ctx
	default:
		return nil, errors.New("got non-file transfer offer")
	}

	var gotTransitMsg transitMsg
	err = collector.waitFor(&gotTransitMsg)
	if err != nil {
		return nil, err
	}

	transitKey := deriveTransitKey(clientProto.sharedKey, appID)
	transport := newFileTransport(transitKey, appID, c.relayURL())

	sendTransitMsg := func() error {
		defer func() {
			mood := rendezvous.Errory
			if returnErr == nil {
				// don't close our connection in this case
				// wait until the user actually accepts the transfer
				return
			} else if returnErr == errDecryptFailed {
				mood = rendezvous.Scary
			}
			rc.Close(ctx, mood)
		}()

		transitMsg, err := transport.makeTransitMsg()
		if err != nil {
			return fmt.Errorf("make transit msg error: %s", err)
		}

		err = clientProto.WriteAppData(ctx, &genericMessage{
			Transit: transitMsg,
		})
		if err != nil {
			return err
		}
		return nil
	}

	accept := func() error {
		answer := &genericMessage{
			Answer: &answerMsg{
				FileAck: "ok",
			},
		}

		err := clientProto.WriteAppData(ctx, answer)
		if err != nil {
			return err
		}

		fr.transferAccepted = true

		if err := sendTransitMsg(); err != nil {
			return err
		}
		return nil
	}
	fr.acceptTransfer = accept

	if fr.options.offerCondition != nil {
		fr.options.offerCondition(offer, accept, reject)
	}

	// defer actually sending the "ok" message until
	// the caller does a read on the IncomingMessage object.
	initialize := func() error {
		conn, err := transport.connectDirect(&gotTransitMsg)
		if err != nil {
			return err
		}

		if conn == nil {
			conn, err = transport.connectViaRelay(&gotTransitMsg)
			if err != nil {
				return err
			}
		}

		if conn == nil {
			return errors.New("failed to establish connection")
		}

		cryptor := newTransportCryptor(conn, transitKey, "transit_record_sender_key", "transit_record_receiver_key")

		fr.cryptor = cryptor
		fr.sha256 = sha256.New()
		return nil
	}
	fr.initializeTransfer = initialize

	return fr, nil
}

// A IncomingMessage contains information about a payload sent to this wormhole client.
//
// The Type field indicates if the sender sent a single file or a directory.
// If the Type is TransferDirectory then reading from the IncomingMessage will
// read a zip file of the contents of the directory.
type IncomingMessage struct {
	Name string
	Type TransferType
	// Deprecated: TransferBytes has been replaced with with TransferBytes64
	// to allow transfer of >2 GiB files on 32 bit systems
	TransferBytes   int
	TransferBytes64 int64
	// Deprecated: UncompressedBytes has been replaced with UncompressedBytes64
	// to allow transfers of > 2 GiB files on 32 bit systems
	UncompressedBytes   int
	UncompressedBytes64 int64
	FileCount           int

	textReader io.Reader

	transferInitialized bool
	transferAccepted    bool
	initializeTransfer  func() error
	acceptTransfer      func() error
	rejectTransfer      func() error

	cryptor   *transportCryptor
	buf       []byte
	readCount int64
	options   transferOptions
	sha256    hash.Hash

	readErr error

	ctx context.Context
}

// Return true if the msg has finished being read.
func (f *IncomingMessage) ReadDone() bool {
	return f.readCount >= f.UncompressedBytes64
}

// Read the decrypted contents sent to this client.
func (f *IncomingMessage) Read(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}

	switch f.Type {
	case TransferText:
		return f.readText(p)
	case TransferFile, TransferDirectory:
		return f.readCrypt(p)
	default:
		return 0, fmt.Errorf("unknown Receiver type %d", f.Type)
	}
}

func (f *IncomingMessage) readText(p []byte) (int, error) {
	return f.textReader.Read(p)
}

// Reject an incoming file or directory transfer. This must be
// called before any calls to Read. This does nothing for
// text message transfers.
func (f *IncomingMessage) Reject() error {
	switch f.Type {
	case TransferFile, TransferDirectory:
	default:
		return errors.New("can only reject File and Directory transfers")
	}

	if f.readErr != nil {
		return f.readErr
	}

	if f.transferInitialized {
		return errors.New("cannot Reject after calls to Read")
	}

	f.transferInitialized = true
	f.rejectTransfer()

	return nil
}

func (f *IncomingMessage) readCrypt(p []byte) (int, error) {
	if f.readErr != nil {
		return 0, f.readErr
	}

	if err := f.ctx.Err(); err != nil {
		f.readErr = err
		if f.cryptor != nil {
			f.cryptor.Close()
		}
		return 0, err
	}

	if !f.transferAccepted {
		if f.options.offerCondition != nil {
			// TODO: refactor into var
			return 0, errors.New("refusing to transfer: conditional offer has not yet been accepted by us")
		}

		f.transferAccepted = true
		if err := f.acceptTransfer(); err != nil {
			return 0, err
		}
	}

	if !f.transferInitialized {
		f.transferInitialized = true
		err := f.initializeTransfer()
		if err != nil {
			return 0, err
		}
	}

	if len(f.buf) == 0 {
		rec, err := f.cryptor.readRecord()
		if err == io.EOF {
			f.readErr = io.ErrUnexpectedEOF
			return 0, f.readErr
		} else if err != nil {
			f.readErr = err
			return 0, err
		}
		f.buf = rec
	}

	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	f.readCount += int64(n)
	f.updateProgress()
	f.sha256.Write(p[:n])
	if f.readCount >= f.TransferBytes64 {
		f.readErr = io.EOF

		sum := f.sha256.Sum(nil)
		ack := fileTransportAck{
			Ack:    "ok",
			SHA256: fmt.Sprintf("%x", sum),
		}

		msg, _ := json.Marshal(ack)
		f.cryptor.writeRecord(msg)
		f.cryptor.Close()
	}

	return n, nil
}

func (f *IncomingMessage) updateProgress() {
	if f.options.progressFunc != nil {
		// NB: f.readCount can be > f.UncompressedBytes64.
		f.options.progressFunc(f.readCount, f.UncompressedBytes64)
	}
}
