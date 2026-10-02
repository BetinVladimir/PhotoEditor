package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"io"
	"net"
	netmail "net/mail"
	"strings"
	"testing"
	"time"
)

func TestSMTPAcceptDespiteLostQuit(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	captured := make(chan string, 1)
	fail := make(chan error, 1)
	go func() {
		conn, e := listener.Accept()
		if e != nil {
			fail <- e
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(5 * time.Second))
		write := func(s string) { io.WriteString(conn, s+"\r\n") }
		write("220 test ESMTP")
		r := bufio.NewReader(conn)
		for {
			line, e := r.ReadString('\n')
			if e != nil {
				fail <- e
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO"):
				write("250 test")
			case strings.HasPrefix(line, "MAIL"), strings.HasPrefix(line, "RCPT"):
				write("250 ok")
			case strings.HasPrefix(line, "DATA"):
				write("354 send data")
				var body strings.Builder
				for {
					line, e = r.ReadString('\n')
					if e != nil {
						fail <- e
						return
					}
					if line == ".\r\n" {
						break
					}
					body.WriteString(line)
				}
				captured <- body.String()
				write("250 accepted")
				return
			default:
				write("500 unsupported")
			}
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	sender := SMTP{Config: Config{Host: "127.0.0.1", Port: port, From: "photo@example.test", TLSMode: "plain", Development: true}}
	message, err := OTPMessage("user@example.test", "<test@example.test>", "ru", "123456")
	if err != nil {
		t.Fatal(err)
	}
	if err = sender.Send(context.Background(), message); err != nil {
		t.Fatal(err)
	}
	select {
	case raw := <-captured:
		parsed, e := netmail.ReadMessage(strings.NewReader(raw))
		if e != nil {
			t.Fatal(e)
		}
		body, e := io.ReadAll(base64.NewDecoder(base64.StdEncoding, parsed.Body))
		if e != nil || !strings.Contains(string(body), "123456") {
			t.Fatal("bad body", e)
		}
		if parsed.Header.Get("Message-ID") != message.ID {
			t.Fatal("message ID changed")
		}
	case e := <-fail:
		t.Fatal(e)
	case <-time.After(5 * time.Second):
		t.Fatal("SMTP timeout")
	}
}
func TestSMTPRejectUnsafe(t *testing.T) {
	for _, c := range []Config{{Host: "remote.example", Port: 25, From: "a@example.test", TLSMode: "plain", Development: true}, {Host: "127.0.0.1", Port: 25, From: "a@example.test", TLSMode: "plain"}, {Host: "127.0.0.1", Port: 25, From: "a@example.test\r\nBcc: b@example.test", TLSMode: "starttls"}} {
		if err := c.Validate(); err == nil {
			t.Fatal("unsafe SMTP accepted")
		}
	}
}

func TestInvitationMessageDoesNotPutTokenInHeaders(t *testing.T) {
	m, err := InviteMessage("user@example.test", "<invite@example.test>", "ru", "Studio", "secret-token")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(m.Subject, "secret-token") || !strings.Contains(m.Body, "secret-token") || m.ID != "<invite@example.test>" {
		t.Fatal("invitation message does not preserve token confidentiality contract")
	}
}
