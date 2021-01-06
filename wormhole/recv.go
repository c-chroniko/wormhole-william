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
	"log"

	"github.com/psanford/wormhole-william/internal/crypto"
	"github.com/psanford/wormhole-william/rendezvous"
)

// Receive receives a message sent by a wormhole client.
//
// It returns an IncomingMessage with metadata about the payload being sent.
// To read the contents of the message call IncomingMessage.Read().
func (c *Client) Receive(ctx context.Context, code string) (fr *IncomingMessage, returnErr error) {
	fmt.Println("hello from client.Receive!")
	sideID := crypto.RandSideID()
	appID := c.appID()
	rc := rendezvous.NewClient(c.url(), sideID, appID)

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

	fmt.Println("one")
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

	fmt.Println("two")
	clientProto := newClientProtocol(ctx, rc, sideID, appID)

	fmt.Println("three")
	err = clientProto.WritePake(ctx, code)
	if err != nil {
		return nil, err
	}

	fmt.Println("four")
	err = clientProto.ReadPake()
	if err != nil {
		return nil, err
	}

	fmt.Println("five")
	err = clientProto.WriteVersion(ctx)
	if err != nil {
		return nil, err
	}

	fmt.Println("six")
	_, err = clientProto.ReadVersion()
	if err != nil {
		return nil, err
	}

	fmt.Println("seven")
	if c.VerifierOk != nil {
		verifier, err := clientProto.Verifier()
		if err != nil {
			return nil, err
		}

		fmt.Println("eight")
		if ok := c.VerifierOk(hex.EncodeToString(verifier)); !ok {
			errMsg := "sender rejected verification check, abandoned transfer"
			writeErr := clientProto.WriteAppData(ctx, &genericMessage{
				Error: &errMsg,
			})
			if writeErr != nil {
				return nil, writeErr
			}

			fmt.Println("nine")
			return nil, errors.New(errMsg)
		}
	}

	fmt.Println("ten")
	collector, err := clientProto.Collect(collectOffer, collectTransit)
	if err != nil {
		return nil, err
	}
	defer collector.close()

	fmt.Println("eleven")
	var offer offerMsg
	err = collector.waitFor(&offer)
	fmt.Println("eleven.1")
	if err != nil {
		fmt.Println("eleven.2")
		return nil, err
	}
	fmt.Println("eleven.3")

	fr = &IncomingMessage{}

	fmt.Println("twelve")
	if offer.Message != nil {
		answer := genericMessage{
			Answer: &answerMsg{
				MessageAck: "ok",
			},
		}

		fmt.Println("thirteen")
		err = clientProto.WriteAppData(ctx, &answer)
		if err != nil {
			return nil, err
		}

		fmt.Println("fourteen")
		rc.Close(ctx, rendezvous.Happy)

		fr.Type = TransferText
		fr.textReader = bytes.NewReader([]byte(*offer.Message))
		return fr, nil
	} else if offer.File != nil {
		fmt.Println("fifteen")
		fr.Type = TransferFile
		fr.Name = offer.File.FileName
		fr.TransferBytes = int(offer.File.FileSize)
		fr.TransferBytes64 = offer.File.FileSize
		fr.UncompressedBytes = int(offer.File.FileSize)
		fr.UncompressedBytes64 = offer.File.FileSize
		fr.FileCount = 1
	} else if offer.Directory != nil {
		fmt.Println("sixteen")
		fr.Type = TransferDirectory
		fr.Name = offer.Directory.Dirname
		fr.TransferBytes = int(offer.Directory.ZipSize)
		fr.TransferBytes64 = offer.Directory.ZipSize
		fr.UncompressedBytes = int(offer.Directory.NumBytes)
		fr.UncompressedBytes64 = offer.Directory.NumBytes
		fr.FileCount = int(offer.Directory.NumFiles)
	} else {
		fmt.Println("seventeen")
		return nil, errors.New("got non-file transfer offer")
	}

	fmt.Println("eighteen")
	var gotTransitMsg transitMsg
	err = collector.waitFor(&gotTransitMsg)
	if err != nil {
		return nil, err
	}

	fmt.Println("newFileTransport!")
	transitKey := deriveTransitKey(clientProto.sharedKey, appID)
	transport := newFileTransport(transitKey, appID, c.relayAddr())
	fmt.Println("post newFileTransport!")

	transitMsg, err := transport.makeTransitMsg()
	if err != nil {
		return nil, fmt.Errorf("make transit msg error: %s", err)
	}

	fmt.Println("clientProto.WriteAppData!")
	err = clientProto.WriteAppData(ctx, &genericMessage{
		Transit: transitMsg,
	})
	fmt.Println("post clientProto.WriteAppData!")
	if err != nil {
		return nil, err
	}

	reject := func() (initErr error) {
		defer func() {
			mood := rendezvous.Errory
			if returnErr == nil {
				mood = rendezvous.Happy
			} else if returnErr == errDecryptFailed {
				mood = rendezvous.Scary
			}
			rc.Close(ctx, mood)
		}()

		var errStr = "transfer rejected"
		answer := &genericMessage{
			Error: &errStr,
		}
		ctx := context.Background()

		err = clientProto.WriteAppData(ctx, answer)
		if err != nil {
			return err
		}

		return nil
	}

	// defer actually sending the "ok" message until
	// the caller does a read on the IncomingMessage object.
	acceptAndInitialize := func() (initErr error) {
		defer func() {
			mood := rendezvous.Errory
			if returnErr == nil {
				mood = rendezvous.Happy
			} else if returnErr == errDecryptFailed {
				mood = rendezvous.Scary
			}
			rc.Close(ctx, mood)
		}()

		answer := &genericMessage{
			Answer: &answerMsg{
				FileAck: "ok",
			},
		}
		ctx := context.Background()

		err = clientProto.WriteAppData(ctx, answer)
		if err != nil {
			return err
		}

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

	fr.initializeTransfer = acceptAndInitialize
	fr.rejectTransfer = reject

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
	initializeTransfer  func() error
	rejectTransfer      func() error

	cryptor   *transportCryptor
	buf       []byte
	readCount int64
	sha256    hash.Hash

	readErr error
}

// Read the decripted contents sent to this client.
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
			log.Printf("unexpected eof! reclen=%d totallen=%d", len(rec), f.readCount)
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
