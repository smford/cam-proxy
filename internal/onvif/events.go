package onvif

import (
	"context"
	"encoding/xml"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// Event represents a normalized camera event.
type Event struct {
	CameraID  string    `json:"camera_id"`
	Type      string    `json:"type"`      // e.g. "motion", "tamper", "input"
	State     bool      `json:"state"`     // true = active/detected, false = cleared
	RawTopic  string    `json:"raw_topic"`
	Timestamp time.Time `json:"timestamp"`
}

// EventHandler is called when a normalized ONVIF event is received.
type EventHandler func(event Event)

// CreatePullPointSubscription initiates an ONVIF PullPoint subscription.
func (d *Device) CreatePullPointSubscription(ctx context.Context) (string, error) {
	eventsURL := d.GetEventsURL(ctx)

	body := `
    <tev:CreatePullPointSubscription>
      <tev:InitialTerminationTime>PT120S</tev:InitialTerminationTime>
    </tev:CreatePullPointSubscription>`

	respBytes, err := d.SendSOAP(ctx, eventsURL, body)
	if err != nil {
		return "", fmt.Errorf("create pull point subscription: %w", err)
	}

	type CreatePullPointResponse struct {
		XMLName xml.Name `xml:"Envelope"`
		Body    struct {
			Response struct {
				SubscriptionReference struct {
					Address string `xml:"Address"`
				} `xml:"SubscriptionReference"`
			} `xml:"CreatePullPointSubscriptionResponse"`
		} `xml:"Body"`
	}

	var parsed CreatePullPointResponse
	if err := xml.Unmarshal(respBytes, &parsed); err != nil {
		return "", fmt.Errorf("parsing subscription reference: %w", err)
	}

	addr := strings.TrimSpace(parsed.Body.Response.SubscriptionReference.Address)
	if addr == "" {
		return "", fmt.Errorf("empty subscription reference address returned")
	}

	return d.ResolveSnapshotURI(addr)
}

// PullMessages requests pending messages from the subscription PullPoint endpoint.
func (d *Device) PullMessages(ctx context.Context, subscriptionURL string, timeout time.Duration) ([]Event, error) {
	timeoutStr := fmt.Sprintf("PT%dS", int(timeout.Seconds()))
	if timeout <= 0 {
		timeoutStr = "PT5S"
	}

	body := fmt.Sprintf(`
    <tev:PullMessages>
      <tev:Timeout>%s</tev:Timeout>
      <tev:MessageLimit>10</tev:MessageLimit>
    </tev:PullMessages>`, timeoutStr)

	respBytes, err := d.SendSOAP(ctx, subscriptionURL, body)
	if err != nil {
		return nil, err
	}

	return parseNotificationMessages(respBytes)
}

// StartEventLoop continuously polls the ONVIF PullPoint and feeds events to handler.
func (d *Device) StartEventLoop(ctx context.Context, cameraID string, handler EventHandler) {
	go func() {
		backoff := 2 * time.Second
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}

			subURL, err := d.CreatePullPointSubscription(ctx)
			if err != nil {
				slog.Warn("failed to create PullPoint subscription", "camera_id", cameraID, "err", err)
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
					if backoff < 30*time.Second {
						backoff *= 2
					}
					continue
				}
			}

			backoff = 2 * time.Second
			slog.Info("ONVIF PullPoint subscription established", "camera_id", cameraID, "url", subURL)

			// Polling loop for active subscription
			for {
				select {
				case <-ctx.Done():
					return
				default:
				}

				events, err := d.PullMessages(ctx, subURL, 5*time.Second)
				if err != nil {
					slog.Debug("pull messages error (subscription may have expired)", "camera_id", cameraID, "err", err)
					break // break out of inner loop to renew subscription
				}

				for _, ev := range events {
					ev.CameraID = cameraID
					handler(ev)
				}
			}
		}
	}()
}

// XML structs for parsing PullMessagesResponse
type pullMessagesEnvelope struct {
	XMLName xml.Name `xml:"Envelope"`
	Body    struct {
		Response struct {
			NotificationMessage []struct {
				Topic struct {
					Value string `xml:",chardata"`
				} `xml:"Topic"`
				Message struct {
					Message struct {
						UtcTime string `xml:"UtcTime,attr"`
						Data    struct {
							SimpleItem []struct {
								Name  string `xml:"Name,attr"`
								Value string `xml:"Value,attr"`
							} `xml:"SimpleItem"`
						} `xml:"Data"`
					} `xml:"Message"`
				} `xml:"Message"`
			} `xml:"NotificationMessage"`
		} `xml:"PullMessagesResponse"`
	} `xml:"Body"`
}

func parseNotificationMessages(xmlData []byte) ([]Event, error) {
	var env pullMessagesEnvelope
	if err := xml.Unmarshal(xmlData, &env); err != nil {
		return nil, err
	}

	var events []Event
	for _, nm := range env.Body.Response.NotificationMessage {
		topic := strings.TrimSpace(nm.Topic.Value)
		var state bool
		foundState := false

		for _, item := range nm.Message.Message.Data.SimpleItem {
			name := strings.ToLower(item.Name)
			if name == "state" || name == "ismotion" || name == "active" || name == "logicalstate" {
				val := strings.ToLower(strings.TrimSpace(item.Value))
				state = (val == "true" || val == "1" || val == "active")
				foundState = true
				break
			}
		}

		if !foundState {
			continue
		}

		evType := "motion"
		lowerTopic := strings.ToLower(topic)
		if strings.Contains(lowerTopic, "tamper") {
			evType = "tamper"
		} else if strings.Contains(lowerTopic, "digitalinput") || strings.Contains(lowerTopic, "input") {
			evType = "input"
		} else if strings.Contains(lowerTopic, "line") {
			evType = "line_cross"
		}

		events = append(events, Event{
			Type:      evType,
			State:     state,
			RawTopic:  topic,
			Timestamp: time.Now().UTC(),
		})
	}

	return events, nil
}
