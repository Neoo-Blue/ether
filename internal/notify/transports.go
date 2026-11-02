package notify

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"strings"
	"time"
)

// post sends one notification over HTTP. Errors never echo the endpoint,
// token or response body: any of them may hold a credential.
func post(ctx context.Context, s *Service, endpoint string, body []byte, contentType string, headers map[string]string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("invalid notification request")
	}
	req.Header.Set("Content-Type", contentType)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.opts.Client.Do(req)
	if err != nil {
		return errors.New("notification transport failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 65536))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("notification endpoint returned HTTP %d", resp.StatusCode)
	}
	return nil
}

func postJSON(ctx context.Context, s *Service, endpoint string, payload any, headers map[string]string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("encode notification failed")
	}
	return post(ctx, s, endpoint, body, "application/json", headers)
}

func bearerHeaders(c Channel) map[string]string {
	if tok := c.Secret("token"); tok != "" {
		return map[string]string{"Authorization": "Bearer " + tok}
	}
	return nil
}

func deliverWebhook(ctx context.Context, s *Service, c Channel, e Event) error {
	return postJSON(ctx, s, c.Secret("endpoint"), e, bearerHeaders(c))
}

func deliverDiscord(ctx context.Context, s *Service, c Channel, e Event) error {
	return postJSON(ctx, s, c.Secret("endpoint"),
		map[string]any{"content": clip(e.Title+"\n"+e.Message, 1900), "allowed_mentions": map[string]any{"parse": []string{}}},
		bearerHeaders(c))
}

func deliverSlack(ctx context.Context, s *Service, c Channel, e Event) error {
	return postJSON(ctx, s, c.Secret("endpoint"),
		map[string]string{"text": clip(e.Title+"\n"+e.Message, 3900)}, bearerHeaders(c))
}

func deliverTelegram(ctx context.Context, s *Service, c Channel, e Event) error {
	endpoint := "https://api.telegram.org/bot" + c.Secret("token") + "/sendMessage"
	return postJSON(ctx, s, endpoint,
		map[string]string{"chat_id": c.Setting("target"), "text": clip(e.Title+"\n"+e.Message, 4000)}, nil)
}

func deliverGotify(ctx context.Context, s *Service, c Channel, e Event) error {
	var headers map[string]string
	if tok := c.Secret("token"); tok != "" {
		headers = map[string]string{"X-Gotify-Key": tok}
	}
	return postJSON(ctx, s, c.Secret("endpoint"),
		map[string]any{"title": e.Title, "message": e.Message, "priority": 5}, headers)
}

func deliverNtfy(ctx context.Context, s *Service, c Channel, e Event) error {
	headers := bearerHeaders(c)
	if headers == nil {
		headers = map[string]string{}
	}
	headers["Title"] = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, e.Title)
	return post(ctx, s, c.Secret("endpoint"), []byte(e.Message), "text/plain; charset=utf-8", headers)
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}

func sendSMTP(ctx context.Context, c Channel, e Event) error {
	return sendSMTPWithTLS(ctx, c, e, nil)
}

func sendSMTPWithTLS(ctx context.Context, c Channel, e Event, trusted *tls.Config) error {
	endpoint := c.Secret("endpoint")
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil {
		return errors.New("invalid SMTP address")
	}
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if trusted != nil {
		tlsConfig = trusted.Clone()
		tlsConfig.ServerName = host
	}
	dialer := net.Dialer{Timeout: 15 * time.Second}
	var conn net.Conn
	mode := c.Setting("smtpMode")
	implicitTLS := mode == "tls" || (mode == "" && port == "465")
	if implicitTLS {
		conn, err = (&tls.Dialer{NetDialer: &dialer, Config: tlsConfig}).DialContext(ctx, "tcp", endpoint)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", endpoint)
	}
	if err != nil {
		return errors.New("SMTP connection failed")
	}
	defer conn.Close()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	deadline := time.Now().Add(20 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	cl, err := smtp.NewClient(conn, host)
	if err != nil {
		return errors.New("SMTP greeting failed")
	}
	defer cl.Close()
	if !implicitTLS {
		if err := cl.StartTLS(tlsConfig); err != nil {
			return errors.New("SMTP requires TLS")
		}
	}
	if username := c.Secret("username"); username != "" {
		if err := cl.Auth(smtp.PlainAuth("", username, c.Secret("password"), host)); err != nil {
			return errors.New("SMTP authentication failed")
		}
	}
	from, err := mail.ParseAddress(c.Setting("from"))
	if err != nil {
		return errors.New("invalid sender email address")
	}
	recipients, err := mail.ParseAddressList(c.Setting("to"))
	if err != nil || len(recipients) == 0 {
		return errors.New("invalid recipient email addresses")
	}
	if cl.Mail(from.Address) != nil {
		return errors.New("SMTP sender rejected")
	}
	for _, to := range recipients {
		if cl.Rcpt(to.Address) != nil {
			return errors.New("SMTP recipient rejected")
		}
	}

	w, err := cl.Data()
	if err != nil {
		return errors.New("SMTP DATA failed")
	}
	subject := mime.QEncoding.Encode("utf-8", strings.NewReplacer("\r", " ", "\n", " ").Replace(e.Title))
	contentType, body := "text/plain", e.Message
	if e.HTML != "" {
		contentType = "text/html"
		body = e.HTML
	}
	var encoded bytes.Buffer
	qp := quotedprintable.NewWriter(&encoded)
	_, _ = qp.Write([]byte(body))
	_ = qp.Close()
	_, err = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMIME-Version: 1.0\r\nContent-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n%s\r\n", from.String(), c.Setting("to"), subject, time.Now().Format(time.RFC1123Z), contentType, encoded.String())
	if err != nil {
		return errors.New("SMTP write failed")
	}

	if w.Close() != nil {
		return errors.New("SMTP message rejected")
	}
	_ = cl.Quit()
	return nil
}
