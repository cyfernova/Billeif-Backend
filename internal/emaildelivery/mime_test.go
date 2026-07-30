package emaildelivery

import (
	"bytes"
	"strings"
	"testing"
)

func TestBuildRawMessageCreatesBoundedPDFMIME(t *testing.T) {
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("invoice"), 20)...)

	raw, err := buildRawMessage(RawMessage{
		Source:    "Billeif <billing@example.com>",
		Recipient: "buyer@example.com",
		Subject:   "Invoice INV/26-27/000001",
		TextBody:  "Your invoice is attached.",
		Filename:  "INV_26-27_000001.pdf",
		PDF:       pdf,
	}, "billeif-test-boundary")

	if err != nil {
		t.Fatalf("build raw message: %v", err)
	}
	message := string(raw)
	for _, want := range []string{
		"From: Billeif <billing@example.com>\r\n",
		"To: buyer@example.com\r\n",
		"Subject: Invoice INV/26-27/000001\r\n",
		`Content-Type: multipart/mixed; boundary="billeif-test-boundary"`,
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Type: application/pdf; name=INV_26-27_000001.pdf",
		"Content-Disposition: attachment; filename=INV_26-27_000001.pdf",
		"Content-Transfer-Encoding: base64",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("raw message missing %q:\n%s", want, message)
		}
	}
	for _, line := range strings.Split(message, "\r\n") {
		if len(line)+2 > maxMIMELineBytes {
			t.Fatalf("MIME line is %d bytes, limit %d", len(line)+2, maxMIMELineBytes)
		}
	}
}

func TestBuildRawMessageWrapsBase64At76Characters(t *testing.T) {
	pdf := append([]byte("%PDF-1.7\n"), bytes.Repeat([]byte("x"), 300)...)

	raw, err := buildRawMessage(RawMessage{
		Source: "Billeif <billing@example.com>", Recipient: "buyer@example.com",
		Subject: "Invoice INV-1", TextBody: "Attached.", Filename: "INV-1.pdf", PDF: pdf,
	}, "billeif-test-boundary")

	if err != nil {
		t.Fatalf("build raw message: %v", err)
	}
	parts := strings.Split(string(raw), "Content-Transfer-Encoding: base64\r\n\r\n")
	if len(parts) != 2 {
		t.Fatalf("base64 part count = %d", len(parts))
	}
	body := strings.Split(parts[1], "\r\n--billeif-test-boundary--")[0]
	for _, line := range strings.Split(strings.TrimSpace(body), "\r\n") {
		if len(line) > 76 {
			t.Fatalf("base64 line is %d chars", len(line))
		}
	}
}

func TestBuildRawMessageRejectsInvalidPDFAndHeaders(t *testing.T) {
	valid := RawMessage{
		Source: "Billeif <billing@example.com>", Recipient: "buyer@example.com",
		Subject: "Invoice INV-1", TextBody: "Attached.", Filename: "INV-1.pdf",
		PDF: []byte("%PDF-1.7\nbody"),
	}
	tests := []struct {
		name   string
		mutate func(*RawMessage)
	}{
		{name: "not PDF", mutate: func(message *RawMessage) { message.PDF = []byte("not-pdf") }},
		{name: "oversized PDF", mutate: func(message *RawMessage) {
			message.PDF = append([]byte("%PDF-"), make([]byte, maxPDFBytes)...)
		}},
		{name: "recipient header injection", mutate: func(message *RawMessage) {
			message.Recipient = "buyer@example.com\r\nBcc: attacker@example.com"
		}},
		{name: "subject header injection", mutate: func(message *RawMessage) {
			message.Subject = "Invoice\r\nBcc: attacker@example.com"
		}},
		{name: "invalid boundary", mutate: func(message *RawMessage) {}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			boundary := "billeif-test-boundary"
			if test.name == "invalid boundary" {
				boundary = "bad\r\nboundary"
			}

			if _, err := buildRawMessage(candidate, boundary); err == nil {
				t.Fatal("invalid MIME input returned nil")
			}
		})
	}
}

func TestSafePDFFilenameStripsUnsafeCharacters(t *testing.T) {
	if got := safePDFFilename("../../INV/26\r\nBcc:evil"); got != "INV_26_Bcc_evil.pdf" {
		t.Fatalf("safe filename = %q", got)
	}
	if got := safePDFFilename("   "); got != "invoice.pdf" {
		t.Fatalf("fallback filename = %q", got)
	}
	longUnicode := "x" + strings.Repeat("ब", 200)
	got := safePDFFilename(longUnicode)
	if !strings.HasSuffix(got, ".pdf") || strings.ToValidUTF8(got, "") != got {
		t.Fatalf("unicode filename is not valid UTF-8: %q", got)
	}
	if len([]rune(strings.TrimSuffix(got, ".pdf"))) > 180 {
		t.Fatalf("unicode filename exceeds rune cap: %d", len([]rune(got)))
	}
}
