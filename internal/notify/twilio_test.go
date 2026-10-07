package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTwilioSender(t *testing.T) {
	var got *http.Request
	status := http.StatusCreated
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		got = r
		w.WriteHeader(status)
		w.Write([]byte(`{"code": 21211, "message": "Invalid 'To' Phone Number"}`))
	}))
	defer srv.Close()

	s := &TwilioSender{AccountSID: "AC1", AuthToken: "tok", From: "+14155238886", BaseURL: srv.URL,
		ContentSIDs: map[string]string{"reminder": "HX1"}}

	if err := s.Send(context.Background(), Message{To: "+447700900123", Kind: "review", Body: "hi"}); err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/2010-04-01/Accounts/AC1/Messages.json" || got.PostForm.Get("Body") != "hi" ||
		got.PostForm.Get("To") != "whatsapp:+447700900123" || got.PostForm.Get("From") != "whatsapp:+14155238886" {
		t.Errorf("bad request: %s %v", got.URL.Path, got.PostForm)
	}
	if u, p, _ := got.BasicAuth(); u != "AC1" || p != "tok" {
		t.Error("missing basic auth")
	}

	s.Send(context.Background(), Message{To: "+447700900123", Kind: "reminder", Vars: []string{"Ada", "Cut"}})
	if got.PostForm.Get("ContentSid") != "HX1" || got.PostForm.Get("ContentVariables") != `{"1":"Ada","2":"Cut"}` || got.PostForm.Has("Body") {
		t.Errorf("template not used: %v", got.PostForm)
	}

	status = http.StatusBadRequest
	if err := s.Send(context.Background(), Message{To: "+1", Kind: "review"}); !IsPermanent(err) {
		t.Errorf("400 should be permanent, got %v", err)
	}
	status = http.StatusServiceUnavailable
	if err := s.Send(context.Background(), Message{To: "+1", Kind: "review"}); err == nil || IsPermanent(err) {
		t.Errorf("503 should be retryable, got %v", err)
	}
}
