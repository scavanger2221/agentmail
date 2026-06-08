package smtp

import (
	"crypto/tls"
	"fmt"
	"net/smtp"
	"strings"
)

// Message represents an email to send.
type Message struct {
	From        string
	To          []string
	Cc          []string
	Subject     string
	Body        string
	BodyHTML    string
	Attachments []string
	InReplyTo   string
	References  string
}

// Client wraps an SMTP connection.
type Client struct {
	host     string
	port     int
	username string
	password string
}

// NewClient creates a new SMTP client.
func NewClient(host string, port int, username, password string) *Client {
	return &Client{
		host:     host,
		port:     port,
		username: username,
		password: password,
	}
}

// Send sends an email message.
func (c *Client) Send(msg *Message) error {
	addr := fmt.Sprintf("%s:%d", c.host, c.port)

	// Build headers
	var header strings.Builder
	header.WriteString(fmt.Sprintf("From: %s\r\n", msg.From))
	header.WriteString(fmt.Sprintf("To: %s\r\n", strings.Join(msg.To, ", ")))
	if len(msg.Cc) > 0 {
		header.WriteString(fmt.Sprintf("Cc: %s\r\n", strings.Join(msg.Cc, ", ")))
	}
	header.WriteString(fmt.Sprintf("Subject: %s\r\n", msg.Subject))
	header.WriteString("MIME-Version: 1.0\r\n")

	if msg.InReplyTo != "" {
		header.WriteString(fmt.Sprintf("In-Reply-To: %s\r\n", msg.InReplyTo))
	}
	if msg.References != "" {
		header.WriteString(fmt.Sprintf("References: %s\r\n", msg.References))
	}

	// Build simple text/plain body or multipart
	allRecipients := append(msg.To, msg.Cc...)

	if msg.BodyHTML != "" {
		header.WriteString("Content-Type: text/html; charset=\"UTF-8\"\r\n")
		header.WriteString("\r\n")
		header.WriteString(msg.BodyHTML)
	} else {
		header.WriteString("Content-Type: text/plain; charset=\"UTF-8\"\r\n")
		header.WriteString("\r\n")
		header.WriteString(msg.Body)
	}

	// Connect and send
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		InsecureSkipVerify: false,
	})
	if err != nil {
		return fmt.Errorf("tls dial: %w", err)
	}

	client, err := smtp.NewClient(conn, c.host)
	if err != nil {
		return fmt.Errorf("smtp client: %w", err)
	}
	defer client.Close()

	auth := smtp.PlainAuth("", c.username, c.password, c.host)
	if err := client.Auth(auth); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	if err := client.Mail(msg.From); err != nil {
		return fmt.Errorf("mail from: %w", err)
	}

	for _, rcpt := range allRecipients {
		if err := client.Rcpt(rcpt); err != nil {
			return fmt.Errorf("rcpt %s: %w", rcpt, err)
		}
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("data: %w", err)
	}

	_, err = w.Write([]byte(header.String()))
	if err != nil {
		return fmt.Errorf("write: %w", err)
	}

	if err := w.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}

	return client.Quit()
}
