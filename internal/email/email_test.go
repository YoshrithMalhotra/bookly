package email

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestBuildMessage(t *testing.T) {
	m := string(buildMessage("Bookly <a@b.c>", "x@y.z", "Réinitialiser", "line1\nline2", time.Unix(0, 0)))
	if !strings.Contains(m, "Subject: =?utf-8?q?R=C3=A9initialiser?=\r\n") || !strings.HasSuffix(m, "\r\n\r\nline1\r\nline2") {
		t.Errorf("bad message:\n%s", m)
	}
}

func TestRejectsHeaderInjection(t *testing.T) {
	s := &SMTPSender{Host: "localhost", Port: "1"}
	if err := s.Send(context.Background(), "a@b.c\r\nBcc: evil@x", "s", "b"); err == nil {
		t.Error("expected error")
	}
}
