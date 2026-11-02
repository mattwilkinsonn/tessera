package komorebi

import (
	"context"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"time"
)

const socketName = "komorebi.sock"

// Client sends one message on a fresh komorebi socket connection.
type Client struct {
	dataDir string
}

func NewClient(dataDir string) *Client {
	return &Client{dataDir: dataDir}
}

// DataDir returns the configured komorebi data directory.
func DataDir() (string, error) {
	if path, ok := os.LookupEnv("TESS_KOMOREBI_DIR"); ok {
		return path, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("getting user cache directory: %w", err)
	}
	return filepath.Join(cacheDir, "komorebi"), nil
}

// Send writes one command and reads a JSON response only for State and Query.
func (c *Client) Send(ctx context.Context, message SocketMessage) (reply jsontext.Value, err error) {
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, fmt.Errorf("encoding komorebi %s message: %w", message.Type, err)
	}

	dialer := net.Dialer{}
	conn, err := dialer.DialContext(ctx, "unix", filepath.Join(c.dataDir, socketName))
	if err != nil {
		return nil, fmt.Errorf("connecting to komorebi socket: %w", err)
	}
	defer func() {
		if closeErr := conn.Close(); closeErr != nil {
			err = errors.Join(err, fmt.Errorf("closing komorebi socket: %w", closeErr))
		}
	}()

	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetWriteDeadline(deadline); err != nil {
			return nil, fmt.Errorf("setting komorebi socket write deadline: %w", err)
		}
	} else if err := conn.SetWriteDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, fmt.Errorf("setting komorebi socket write deadline: %w", err)
	}
	if err := writeAll(conn, payload); err != nil {
		return nil, fmt.Errorf("writing komorebi %s message: %w", message.Type, err)
	}

	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return nil, errors.New("komorebi socket is not a Unix connection")
	}
	if err := unixConn.CloseWrite(); err != nil {
		return nil, fmt.Errorf("finishing komorebi %s request: %w", message.Type, err)
	}
	if !message.expectsReply() {
		return nil, nil
	}

	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		return nil, fmt.Errorf("setting komorebi socket read deadline: %w", err)
	}
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("reading komorebi %s reply: %w", message.Type, ctx.Err())
	default:
	}

	bytes, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("reading komorebi %s reply: %w", message.Type, err)
	}
	response := jsontext.Value(bytes)
	if !response.IsValid() {
		return nil, fmt.Errorf("decoding komorebi %s reply: invalid JSON", message.Type)
	}
	return response, nil
}

func writeAll(writer io.Writer, payload []byte) error {
	for len(payload) > 0 {
		written, err := writer.Write(payload)
		if err != nil {
			return err
		}
		if written == 0 {
			return io.ErrShortWrite
		}
		payload = payload[written:]
	}
	return nil
}
