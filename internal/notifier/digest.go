package notifier

import (
	"bytes"
	_ "embed"
	"html/template"
	"strings"
	"time"
)

//go:embed templates/digest.html
var digestTemplateRaw string

var digestTmpl = template.Must(template.New("digest").Parse(digestTemplateRaw))

// EnlistedEntry holds one row for the digest "new waitlist entries" section.
type EnlistedEntry struct {
	Firstname string
	Lastname  string
	Email     string
	JoinedAt  string
}

// GrantedEntry holds one row for the digest "access granted" section.
type GrantedEntry struct {
	Firstname string
	Lastname  string
	Email     string
	GrantedAt string
	GrantedBy string
}

// DigestData contains all data required to render the digest email template.
type DigestData struct {
	ProjectName   string
	PeriodStart   string
	PeriodEnd     string
	NewEnlisted   []EnlistedEntry
	NewGranted    []GrantedEntry
	EnlistedCount int
	GrantedCount  int
}

const digestTimeFormat = "2006-01-02 15:04 UTC"

// FormatDigestTime formats a time.Time for display in digest emails.
func FormatDigestTime(t time.Time) string {
	return t.UTC().Format(digestTimeFormat)
}

// RenderDigest renders the digest template to HTML.
func RenderDigest(data DigestData) (string, error) {
	var buf bytes.Buffer
	if err := digestTmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SendDigest sends a digest email to the given recipients. Skips if
// recipients is empty.
func (n *SMTPNotifier) SendDigest(recipients []string, from, subject string, data DigestData) error {
	if len(recipients) == 0 {
		return nil
	}

	body, err := RenderDigest(data)
	if err != nil {
		return err
	}

	msg := buildMIMEMessage(from, strings.Join(recipients, ", "), subject, body)

	for _, rcpt := range recipients {
		if sendErr := n.send(from, rcpt, msg); sendErr != nil {
			n.logger.Warn("digest: failed to send email",
				"error", sendErr, "to", rcpt)
		}
	}
	return nil
}
