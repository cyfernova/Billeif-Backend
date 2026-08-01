package emaildelivery

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"unicode"
)

const (
	maxPDFBytes      = 6 * 1024 * 1024
	maxRawEmailBytes = 10_000_000
	maxMIMELineBytes = 1000
	base64LineBytes  = 76
)

type RawMessage struct {
	Source    string
	Recipient string
	Subject   string
	TextBody  string
	Filename  string
	PDF       []byte
}

func BuildRawMessage(message RawMessage) ([]byte, error) {
	var random [18]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, fmt.Errorf("generate MIME boundary: %w", err)
	}
	return buildRawMessage(message, "billeif-"+hex.EncodeToString(random[:]))
}

func buildRawMessage(message RawMessage, boundary string) ([]byte, error) {
	if err := validateRawMessage(message, boundary); err != nil {
		return nil, err
	}
	filename := safePDFFilename(message.Filename)
	contentType := mime.FormatMediaType("application/pdf", map[string]string{"name": filename})
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": filename})

	var raw bytes.Buffer
	writeHeader := func(name, value string) {
		fmt.Fprintf(&raw, "%s: %s\r\n", name, value)
	}
	writeHeader("From", message.Source)
	writeHeader("To", message.Recipient)
	writeHeader("Subject", mime.QEncoding.Encode("UTF-8", message.Subject))
	writeHeader("MIME-Version", "1.0")
	writeHeader("Content-Type", fmt.Sprintf(`multipart/mixed; boundary="%s"`, boundary))
	raw.WriteString("\r\n")

	fmt.Fprintf(&raw, "--%s\r\n", boundary)
	raw.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	raw.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
	textWriter := quotedprintable.NewWriter(&raw)
	if _, err := textWriter.Write([]byte(message.TextBody)); err != nil {
		return nil, fmt.Errorf("encode text body: %w", err)
	}
	if err := textWriter.Close(); err != nil {
		return nil, fmt.Errorf("close text encoder: %w", err)
	}
	if !bytes.HasSuffix(raw.Bytes(), []byte("\r\n")) {
		raw.WriteString("\r\n")
	}

	fmt.Fprintf(&raw, "--%s\r\n", boundary)
	writeHeader("Content-Type", contentType)
	writeHeader("Content-Disposition", disposition)
	raw.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
	encoded := base64.StdEncoding.EncodeToString(message.PDF)
	for len(encoded) > 0 {
		width := base64LineBytes
		if len(encoded) < width {
			width = len(encoded)
		}
		raw.WriteString(encoded[:width])
		raw.WriteString("\r\n")
		encoded = encoded[width:]
	}
	fmt.Fprintf(&raw, "--%s--\r\n", boundary)

	if raw.Len() > maxRawEmailBytes {
		return nil, errors.New("raw email exceeds SES size limit")
	}
	for _, line := range bytes.Split(raw.Bytes(), []byte("\r\n")) {
		if len(line)+2 > maxMIMELineBytes {
			return nil, errors.New("raw email contains an oversized MIME line")
		}
	}
	return raw.Bytes(), nil
}

func validateRawMessage(message RawMessage, boundary string) error {
	if strings.ContainsAny(boundary, "\r\n\"") || strings.TrimSpace(boundary) == "" {
		return errors.New("invalid MIME boundary")
	}
	if _, err := mail.ParseAddress(message.Source); err != nil ||
		strings.ContainsAny(message.Source, "\r\n") {
		return errors.New("invalid source address")
	}
	recipient, err := mail.ParseAddress(message.Recipient)
	if err != nil || recipient.Address != strings.TrimSpace(message.Recipient) ||
		strings.ContainsAny(message.Recipient, "\r\n") {
		return errors.New("invalid recipient address")
	}
	if strings.TrimSpace(message.Subject) == "" || strings.ContainsAny(message.Subject, "\r\n") {
		return errors.New("invalid email subject")
	}
	if strings.ContainsAny(message.Filename, "\r\n") {
		return errors.New("invalid PDF filename")
	}
	if len(message.PDF) > maxPDFBytes {
		return errors.New("PDF attachment exceeds size limit")
	}
	if len(message.PDF) < len("%PDF-") || !bytes.Equal(message.PDF[:len("%PDF-")], []byte("%PDF-")) {
		return errors.New("attachment is not a PDF")
	}
	return nil
}

func safePDFFilename(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasSuffix(strings.ToLower(value), ".pdf") {
		value = strings.TrimSpace(value[:len(value)-len(".pdf")])
	}
	var normalized strings.Builder
	separator := false
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) ||
			character == '-' || character == '.' {
			normalized.WriteRune(character)
			separator = false
			continue
		}
		if normalized.Len() > 0 && !separator {
			normalized.WriteByte('_')
			separator = true
		}
	}
	base := strings.Trim(normalized.String(), "._-")
	if base == "" {
		base = "invoice"
	}
	runes := []rune(base)
	if len(runes) > 180 {
		base = strings.TrimRight(string(runes[:180]), "._-")
	}
	return base + ".pdf"
}
