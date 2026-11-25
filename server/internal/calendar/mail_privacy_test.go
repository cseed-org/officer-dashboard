package calendar

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
)

func TestSMTPErrorOmitsServerEchoedPersonalContent(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		fmt.Fprint(conn, "220 test SMTP\r\n")
		scan := bufio.NewScanner(conn)
		for scan.Scan() {
			if strings.HasPrefix(scan.Text(), "RCPT") {
				fmt.Fprint(conn, "550 private-person@example.invalid sensitive-provider-detail\r\n")
				return
			}
			fmt.Fprint(conn, "250 test OK\r\n")
		}
	}()
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	s := SMTP{Host: host, Port: port, Sender: "sender@example.invalid"}
	err = s.Send(context.Background(), Message{To: "private-person@example.invalid", Text: "private message"})
	<-done
	if err == nil || !strings.Contains(err.Error(), "550") {
		t.Fatal("SMTP rejection status was not retained")
	}
	if strings.Contains(err.Error(), "@") || strings.Contains(err.Error(), "sensitive-provider-detail") {
		t.Fatal("SMTP error leaks provider-echoed personal content to its callers/logs")
	}
}
