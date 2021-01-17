package test

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/assert"
	"io/ioutil"
	"testing"

	"github.com/psanford/wormhole-william/wormhole"
	"github.com/stretchr/testify/require"
)

func newTestClient() *wormhole.Client {
	return &wormhole.Client{
		AppID:                     "test_app",
		RendezvousURL:             "http://localhost:4000/v1",
		TransitRelayAddress:       "ws://localhost:4001",
		PassPhraseComponentLength: 2,
	}
}

// NB: assumes mailbox and relay servers are running.
func TestIntegration(t *testing.T) {
	ctx := context.Background()

	sender := newTestClient()
	testStr := "testing 123 | Hello World!"
	buf := new(bytes.Buffer)
	buf.WriteString(testStr)

	code, _, err := sender.SendFile(ctx, "test-file.txt", bytes.NewReader(buf.Bytes()))
	require.NoError(t, err)
	require.NotEmpty(t, code)

	receiver := newTestClient()
	msg, err := receiver.Receive(ctx, code)
	require.NoError(t, err)
	require.NotNil(t, msg)

	msgData, err := ioutil.ReadAll(msg)
	require.NoError(t, err)
	require.NotEmpty(t, msgData)

	assert.Equal(t, testStr, string(msgData))
}
