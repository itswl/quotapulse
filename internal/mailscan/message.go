package mailscan

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/emersion/go-message"
	// register GBK / Big5 / ISO-8859 charsets: Chinese billing mails use GBK heavily;
	// without registration subjects and bodies turn to mojibake and no keyword ever
	// matches.
	_ "github.com/emersion/go-message/charset"
)

// maxPartDepth caps MIME nesting: real mails stop at two or three levels; this gate is for crafted deep nesting.
const maxPartDepth = 10

// htmlTag matches HTML tags, replaced with spaces rather than deleted — tags are often the only separator between words.
var htmlTag = regexp.MustCompile(`<[^>]+>`)

// parsedMessage is what scanning needs from one mail.
type parsedMessage struct {
	ID      string // dedup key within this scan
	Subject string
	Sender  string
	Date    string
	Body    string
}

// parseMessage parses one RFC822 raw message. The returned parsedMessage is
// always usable; the error only says the mail is malformed and worth a log
// line.
//
// On an unrecognized charset or transfer encoding message.Read still yields
// a readable Entity, which is used as-is: unknown bytes become replacement
// characters, better than dropping the whole mail — the keywords usually
// survive in the decodable part.
func parseMessage(raw []byte) (parsedMessage, error) {
	ent, err := message.Read(bytes.NewReader(raw))
	if ent == nil {
		// when headers are too broken to parse, scan the raw message as the body —
		// keywords still match. Dedup keys fall back to a digest of the raw bytes, or
		// every broken mail collapses into one ID.
		sum := md5.Sum(raw)
		return parsedMessage{ID: hex.EncodeToString(sum[:]), Body: string(raw)}, fmt.Errorf("Failed to parse email: %w", err)
	}
	return parsedMessage{
		ID:      messageID(&ent.Header),
		Subject: headerText(&ent.Header, "Subject"),
		Sender:  headerText(&ent.Header, "From"),
		Date:    headerText(&ent.Header, "Date"),
		Body:    extractText(ent),
	}, nil
}

// headerText decodes MIME-encoded headers (=?utf-8?B?...?=) and falls back to the
// raw bytes — a half-readable header still beats an empty string; at least logs
// show who sent it.
func headerText(h *message.Header, key string) string {
	value, err := h.Text(key)
	if err != nil {
		return h.Get(key)
	}
	return value
}

// messageID prefers Message-ID and falls back to md5(date|subject|from). Both use
// undecoded raw headers: decoded forms vary by implementation, raw bytes do not,
// and dedup keys must be stable.
func messageID(h *message.Header) string {
	if id := strings.TrimSpace(h.Get("Message-ID")); id != "" {
		return id
	}
	sum := md5.Sum([]byte(h.Get("Date") + "|" + h.Get("Subject") + "|" + h.Get("From")))
	return hex.EncodeToString(sum[:])
}

// extractText extracts the body text: text/plain as-is, text/html stripped of tags, attachments skipped.
func extractText(ent *message.Entity) string {
	var texts []string
	collectText(ent, &texts, 0)
	return strings.Join(texts, "\n")
}

func collectText(ent *message.Entity, texts *[]string, depth int) {
	// containers have no body but must be traversed: leave the attachment decision
	// to leaf parts, or the body of a forwarded mail marked attachment as a whole
	// is lost
	if mr := ent.MultipartReader(); mr != nil {
		if depth >= maxPartDepth {
			return
		}
		for {
			part, err := mr.NextPart()
			if err != nil {
				return // io.EOF, or this mail's part is broken — nothing more to read
			}
			collectText(part, texts, depth+1)
		}
	}

	if disp, _, _ := ent.Header.ContentDisposition(); strings.EqualFold(disp, "attachment") {
		return
	}
	contentType, _, _ := ent.Header.ContentType()
	if contentType != "text/plain" && contentType != "text/html" {
		return
	}

	// even a broken read keeps what was read: keywords in a partial body still count
	body, _ := io.ReadAll(ent.Body)
	text := string(body)
	if contentType == "text/html" {
		text = htmlTag.ReplaceAllString(text, " ")
	}
	if text != "" {
		*texts = append(*texts, text)
	}
}
