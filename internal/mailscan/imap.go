package mailscan

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"

	"github.com/itswl/quotapulse/internal/model"
)

// defaultPort is the IMAPS port: an unset (zero) port means 993.
const defaultPort = 993

// mailConn is every IMAP capability needed to scan one mailbox.
//
// It exists as an interface so the scan flow tests without a real server —
// protocol details stay locked in this file and the layer above only knows
// "give me recent message numbers" and "fetch these raw messages".
type mailConn interface {
	// Search returns the message numbers since a date, ordered by the server (oldest first).
	Search(since time.Time) ([]uint32, error)
	// Fetch fetches raw messages in bulk. Fewer may return than requested: the server simply has no more.
	Fetch(nums []uint32) ([][]byte, error)
	Close() error
}

// dialFunc establishes a logged-in connection with INBOX selected. Scanner swaps in a fake for tests.
type dialFunc func(ctx context.Context, m model.Mailbox, timeout time.Duration) (mailConn, error)

type imapConn struct {
	client *imapclient.Client
}

// dialIMAP connects, logs in and selects INBOX; any failure closes the
// connection before returning the error.
//
// The timeout only covers through "INBOX selected" and is then dropped: a
// mailbox may pull thousands of mails, and holding one HTTP-request-sized
// timeout over a whole scan means big mailboxes never finish.
func dialIMAP(ctx context.Context, m model.Mailbox, timeout time.Duration) (mailConn, error) {
	addr := net.JoinHostPort(m.Host, strconv.Itoa(mailboxPort(m)))

	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("Failed to connect to %s: %w", addr, err)
	}
	if timeout > 0 {
		// avoid hanging in the handshake when the port answers but the server stays silent
		_ = conn.SetDeadline(time.Now().Add(timeout))
	}
	if m.UseSSL {
		tlsConn := tls.Client(conn, &tls.Config{ServerName: m.Host, NextProtos: []string{"imap"}})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			conn.Close()
			return nil, fmt.Errorf("TLS handshake with %s failed: %w", addr, err)
		}
		conn = tlsConn
	}

	client := imapclient.New(conn, nil)
	if err := client.Login(m.Username, m.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("Failed to log in as %s: %w", m.Username, err)
	}
	if _, err := client.Select("INBOX", nil).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("Failed to open INBOX: %w", err)
	}
	_ = conn.SetDeadline(time.Time{})
	return &imapConn{client: client}, nil
}

func (c *imapConn) Search(since time.Time) ([]uint32, error) {
	// SINCE compares dates only; the server ignores time and timezone
	data, err := c.client.Search(&imap.SearchCriteria{Since: since}, nil).Wait()
	if err != nil {
		return nil, err
	}
	return data.AllSeqNums(), nil
}

func (c *imapConn) Fetch(nums []uint32) ([][]byte, error) {
	if len(nums) == 0 {
		return nil, nil
	}

	// BODY.PEEK[] fetches the whole raw message. PEEK instead of a plain fetch keeps
	// the user's mail unread — a monitoring scan must not touch inbox state.
	section := &imap.FetchItemBodySection{Peek: true}
	messages, err := c.client.Fetch(
		imap.SeqSetNum(nums...),
		&imap.FetchOptions{BodySection: []*imap.FetchItemBodySection{section}},
	).Collect()
	if err != nil {
		return nil, err
	}

	raws := make([][]byte, 0, len(messages))
	for _, msg := range messages {
		if raw := msg.FindBodySection(section); len(raw) > 0 {
			raws = append(raws, raw)
		}
	}
	return raws, nil
}

// Close sends LOGOUT before dropping the connection so the server does not record an abnormal disconnect (some providers throttle for it).
func (c *imapConn) Close() error {
	err := c.client.Logout().Wait()
	c.client.Close()
	return err
}

func mailboxPort(m model.Mailbox) int {
	if m.Port <= 0 {
		return defaultPort
	}
	return m.Port
}
