package notifier

import (
	"bytes"
	"crypto/tls"
	"embed"
	"fmt"
	"html/template"
	"log/slog"
	"net"
	"net/smtp"
	"strconv"
	"time"

	"github.com/tipok/waitinglist/internal/config"
	"github.com/tipok/waitinglist/internal/model"
)

//go:embed templates/access_granted.html
var templateFS embed.FS

// Notifier defines the interface for sending access-granted notifications.
type Notifier interface {
	NotifyAccessGranted(user model.UserEntity, project model.Project)
}

type templateData struct {
	Firstname   string
	Lastname    string
	ProjectName string
}

// SMTPNotifier sends HTML emails via SMTP when users are granted access.
type SMTPNotifier struct {
	host     string
	port     int
	username string
	password string
	useTLS   bool
	tmpl     *template.Template
	logger   *slog.Logger
}

// New creates an SMTPNotifier from the given SMTP config. Returns nil if
// the SMTP host is not configured (notifications disabled).
func New(cfg config.SMTPConfig, logger *slog.Logger) *SMTPNotifier {
	if cfg.Host == "" {
		return nil
	}

	tmpl := template.Must(template.ParseFS(templateFS, "templates/access_granted.html"))

	return &SMTPNotifier{
		host:     cfg.Host,
		port:     cfg.Port,
		username: cfg.Username,
		password: cfg.Password,
		useTLS:   cfg.TLS,
		tmpl:     tmpl,
		logger:   logger,
	}
}

// NotifyAccessGranted sends an access-granted email to the user. Skips
// silently if the project has no emailFrom or emailSubject configured.
func (n *SMTPNotifier) NotifyAccessGranted(user model.UserEntity, project model.Project) {
	if project.Email.From == "" || project.Email.Subject == "" {
		return
	}

	data := templateData{
		Firstname:   user.Firstname,
		Lastname:    user.Lastname,
		ProjectName: project.Name,
	}

	var body bytes.Buffer
	if err := n.tmpl.Execute(&body, data); err != nil {
		n.logger.Warn("notifier: failed to render email template", "error", err, "user_id", user.ID)
		return
	}

	msg := buildMIMEMessage(project.Email.From, user.Email, project.Email.Subject, body.String())

	if err := n.send(project.Email.From, user.Email, msg); err != nil {
		n.logger.Warn("notifier: failed to send email",
			"error", err, "to", user.Email, "project", project.Slug)
	}
}

func (n *SMTPNotifier) send(from, to string, msg []byte) error {
	const dialTimeout = 10 * time.Second

	addr := net.JoinHostPort(n.host, strconv.Itoa(n.port))

	var c *smtp.Client
	var err error

	if n.useTLS {
		dialer := &net.Dialer{Timeout: dialTimeout}
		conn, dialErr := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: n.host})
		if dialErr != nil {
			return fmt.Errorf("tls dial: %w", dialErr)
		}
		c, err = smtp.NewClient(conn, n.host)
		if err != nil {
			return fmt.Errorf("smtp client over tls: %w", err)
		}
	} else {
		conn, dialErr := net.DialTimeout("tcp", addr, dialTimeout)
		if dialErr != nil {
			return fmt.Errorf("smtp dial: %w", dialErr)
		}
		c, err = smtp.NewClient(conn, n.host)
		if err != nil {
			return fmt.Errorf("smtp client: %w", err)
		}
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err = c.StartTLS(&tls.Config{ServerName: n.host}); err != nil {
				_ = c.Close()
				return fmt.Errorf("starttls: %w", err)
			}
		}
	}
	defer func() { _ = c.Close() }()

	if n.username != "" {
		auth := smtp.PlainAuth("", n.username, n.password, n.host)
		if err = c.Auth(auth); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}

	if err = c.Mail(from); err != nil {
		return fmt.Errorf("smtp mail: %w", err)
	}
	if err = c.Rcpt(to); err != nil {
		return fmt.Errorf("smtp rcpt: %w", err)
	}

	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("smtp data: %w", err)
	}
	if _, err = w.Write(msg); err != nil {
		return fmt.Errorf("smtp write: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("smtp close data: %w", err)
	}

	return c.Quit()
}

func buildMIMEMessage(from, to, subject, htmlBody string) []byte {
	var buf bytes.Buffer
	buf.WriteString("From: " + from + "\r\n")
	buf.WriteString("To: " + to + "\r\n")
	buf.WriteString("Subject: " + subject + "\r\n")
	buf.WriteString("MIME-Version: 1.0\r\n")
	buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
	buf.WriteString("\r\n")
	buf.WriteString(htmlBody)
	return buf.Bytes()
}
