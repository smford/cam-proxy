package onvif_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/smford/camstop/internal/onvif"
)

func TestPullPointAndMessages(t *testing.T) {
	// Mock ONVIF server handling CreatePullPointSubscription and PullMessages
	var subServerURL string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/soap+xml; charset=utf-8")

		if r.URL.Path == "/onvif/event_service" {
			resp := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">
  <s:Body>
    <tev:CreatePullPointSubscriptionResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl">
      <tev:SubscriptionReference>
        <Address xmlns="http://www.w3.org/2005/08/addressing">%s/onvif/subscription/1</Address>
      </tev:SubscriptionReference>
    </tev:CreatePullPointSubscriptionResponse>
  </s:Body>
</s:Envelope>`, subServerURL)
			_, _ = fmt.Fprint(w, resp)
			return
		}

		if r.URL.Path == "/onvif/subscription/1" {
			resp := `<?xml version="1.0" encoding="utf-8"?>
<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"
            xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2"
            xmlns:tt="http://www.onvif.org/ver10/schema">
  <s:Body>
    <tev:PullMessagesResponse xmlns:tev="http://www.onvif.org/ver10/events/wsdl">
      <wsnt:NotificationMessage>
        <wsnt:Topic>tns1:RuleEngine/CellMotionDetector/Motion</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-10-01T22:00:00Z">
            <tt:Data>
              <tt:SimpleItem Name="State" Value="true"/>
            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
      <wsnt:NotificationMessage>
        <wsnt:Topic>tns1:VideoSource/TamperAlarm</wsnt:Topic>
        <wsnt:Message>
          <tt:Message UtcTime="2026-10-01T22:00:01Z">
            <tt:Data>
              <tt:SimpleItem Name="State" Value="false"/>
            </tt:Data>
          </tt:Message>
        </wsnt:Message>
      </wsnt:NotificationMessage>
    </tev:PullMessagesResponse>
  </s:Body>
</s:Envelope>`
			_, _ = fmt.Fprint(w, resp)
			return
		}

		http.NotFound(w, r)
	}))
	defer server.Close()
	subServerURL = server.URL

	dev := onvif.NewDevice(server.URL, "admin", "pass")
	subURL, err := dev.CreatePullPointSubscription(context.Background())
	if err != nil {
		t.Fatalf("failed to create subscription: %v", err)
	}

	expectedSub := server.URL + "/onvif/subscription/1"
	if subURL != expectedSub {
		t.Errorf("expected subscription URL %q, got %q", expectedSub, subURL)
	}

	events, err := dev.PullMessages(context.Background(), subURL, 2*time.Second)
	if err != nil {
		t.Fatalf("failed to pull messages: %v", err)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}

	// First event: motion active
	if events[0].Type != "motion" {
		t.Errorf("expected event type 'motion', got %q", events[0].Type)
	}
	if !events[0].State {
		t.Errorf("expected motion state true")
	}

	// Second event: tamper cleared
	if events[1].Type != "tamper" {
		t.Errorf("expected event type 'tamper', got %q", events[1].Type)
	}
	if events[1].State {
		t.Errorf("expected tamper state false")
	}
}
