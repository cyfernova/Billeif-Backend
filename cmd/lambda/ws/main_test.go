package main

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
)

type stubWebSocketTicketConsumer struct {
	ticketValue string
	ticket      *models.WebSocketTicket
	err         error
	consumed    bool
}

func (s *stubWebSocketTicketConsumer) Consume(_ context.Context, ticketValue string) (*models.WebSocketTicket, error) {
	s.ticketValue = ticketValue
	if s.consumed {
		return nil, interfaces.ErrWebSocketTicketInvalid
	}
	s.consumed = true
	return s.ticket, s.err
}

type stubWebSocketConnectionRegistry struct {
	connectionID string
	userID       string
	businessID   string
	calls        int
	heartbeats   []string
	err          error
}

func (s *stubWebSocketConnectionRegistry) RegisterConnectionWithBusiness(_ context.Context, connectionID, userID, businessID string) error {
	s.calls++
	s.connectionID = connectionID
	s.userID = userID
	s.businessID = businessID
	return s.err
}

func (s *stubWebSocketConnectionRegistry) UnregisterConnection(_ context.Context, _ string) error {
	return nil
}

func (s *stubWebSocketConnectionRegistry) SendHeartbeat(_ context.Context, connectionID string) error {
	s.heartbeats = append(s.heartbeats, connectionID)
	return s.err
}

func TestWebSocketHeartbeatRepliesOnlyToGatewayConnection(t *testing.T) {
	for _, body := range []string{`{"action":"ping","connection_id":"other-tenant"}`, `{"type":"ping"}`} {
		connections := &stubWebSocketConnectionRegistry{}
		response := handleWebSocketMessage(context.Background(), events.APIGatewayWebsocketProxyRequest{
			Body: body, RequestContext: events.APIGatewayWebsocketProxyRequestContext{ConnectionID: "authenticated-connection"},
		}, connections, logger.New())
		if response.StatusCode != 200 || len(connections.heartbeats) != 1 || connections.heartbeats[0] != "authenticated-connection" {
			t.Fatalf("response = %#v, heartbeats = %v", response, connections.heartbeats)
		}
	}
}

func TestWebSocketHeartbeatRejectsInvalidMessagesAndReportsDeliveryFailure(t *testing.T) {
	for _, fixture := range []struct {
		body, connectionID string
		deliveryError      error
		status, calls      int
	}{
		{body: "not json", connectionID: "connection", status: 400},
		{body: `{"action":"ping"}`, status: 400},
		{body: `{"action":"unknown"}`, connectionID: "connection", status: 200},
		{body: `{"action":"ping"}`, connectionID: "connection", deliveryError: errors.New("delivery unavailable"), status: 500, calls: 1},
	} {
		connections := &stubWebSocketConnectionRegistry{err: fixture.deliveryError}
		response := handleWebSocketMessage(context.Background(), events.APIGatewayWebsocketProxyRequest{Body: fixture.body, RequestContext: events.APIGatewayWebsocketProxyRequestContext{ConnectionID: fixture.connectionID}}, connections, logger.New())
		if response.StatusCode != fixture.status || len(connections.heartbeats) != fixture.calls {
			t.Fatalf("fixture=%+v, response=%+v, heartbeats=%v", fixture, response, connections.heartbeats)
		}
	}
}

func TestExtractWebSocketTicketAcceptsOnlyTicketParameter(t *testing.T) {
	request := events.APIGatewayWebsocketProxyRequest{QueryStringParameters: map[string]string{
		"ticket":       "  opaque-ticket  ",
		"access_token": "access-token-must-be-ignored",
		"business_id":  "business-must-be-ignored",
	}}
	if got := extractWebSocketTicket(request); got != "opaque-ticket" {
		t.Fatalf("ticket = %q", got)
	}

	request.QueryStringParameters = map[string]string{
		"access_token":  "legacy-token",
		"authorization": "Bearer legacy-token",
	}
	if got := extractWebSocketTicket(request); got != "" {
		t.Fatalf("legacy credential accepted: %q", got)
	}
}

func TestWebSocketConnectUsesTicketBoundSubjectAndBusiness(t *testing.T) {
	tickets := &stubWebSocketTicketConsumer{ticket: &models.WebSocketTicket{
		Subject:    "subject-from-ticket",
		BusinessID: "10000000-0000-0000-0000-000000000001",
	}}
	connections := &stubWebSocketConnectionRegistry{}
	request := events.APIGatewayWebsocketProxyRequest{
		QueryStringParameters: map[string]string{
			"ticket":      "opaque-ticket",
			"business_id": "attacker-business",
			"user_id":     "attacker-subject",
		},
		RequestContext: events.APIGatewayWebsocketProxyRequestContext{ConnectionID: "connection-1"},
	}

	response := handleWebSocketConnect(context.Background(), request, tickets, connections, logger.New())

	if response.StatusCode != 200 {
		t.Fatalf("status = %d, body = %q", response.StatusCode, response.Body)
	}
	if tickets.ticketValue != "opaque-ticket" {
		t.Fatalf("consumed ticket = %q", tickets.ticketValue)
	}
	if connections.calls != 1 || connections.connectionID != "connection-1" ||
		connections.userID != "subject-from-ticket" ||
		connections.businessID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("registered connection = %#v", connections)
	}
}

func TestWebSocketConnectRejectsInvalidAndReusedTickets(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		tickets *stubWebSocketTicketConsumer
	}{
		{
			name: "invalid",
			tickets: &stubWebSocketTicketConsumer{
				err: interfaces.ErrWebSocketTicketInvalid,
			},
		},
		{
			name: "reused",
			tickets: &stubWebSocketTicketConsumer{
				consumed: true,
			},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			connections := &stubWebSocketConnectionRegistry{}
			response := handleWebSocketConnect(context.Background(), events.APIGatewayWebsocketProxyRequest{
				QueryStringParameters: map[string]string{"ticket": "opaque-ticket"},
				RequestContext:        events.APIGatewayWebsocketProxyRequestContext{ConnectionID: "connection-1"},
			}, fixture.tickets, connections, logger.New())
			if response.StatusCode != 401 {
				t.Fatalf("status = %d, body = %q", response.StatusCode, response.Body)
			}
			if connections.calls != 0 {
				t.Fatalf("registration calls = %d", connections.calls)
			}
		})
	}
}

func TestWebSocketConnectReturnsInternalErrorForTicketStoreFailure(t *testing.T) {
	tickets := &stubWebSocketTicketConsumer{err: errors.New("database unavailable")}
	connections := &stubWebSocketConnectionRegistry{}
	response := handleWebSocketConnect(context.Background(), events.APIGatewayWebsocketProxyRequest{
		QueryStringParameters: map[string]string{"ticket": "opaque-ticket"},
		RequestContext:        events.APIGatewayWebsocketProxyRequestContext{ConnectionID: "connection-1"},
	}, tickets, connections, logger.New())
	if response.StatusCode != 500 || connections.calls != 0 {
		t.Fatalf("response = %#v, registrations = %d", response, connections.calls)
	}
}
