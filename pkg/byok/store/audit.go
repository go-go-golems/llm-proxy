package store

import (
	"encoding/json"
	"time"
)

const (
	AuditCredentialCreated       = "credential.created"
	AuditCredentialDeleted       = "credential.deleted"
	AuditTokenMinted             = "token.minted"
	AuditTokenRevoked            = "token.revoked"
	AuditDeviceTokenIssued       = "token.device_issued"
	AuditTokenRotated            = "token.rotated"
	AuditMeterCircuitOpened      = "meter.circuit_opened"
	AuditMeterCircuitClosed      = "meter.circuit_closed"
	AuditAuthTransactionCreated  = "oidc.auth_transaction_created"
	AuditAuthTransactionConsumed = "oidc.auth_transaction_consumed"
	AuditSessionCreated          = "session.created"
	AuditSessionRevoked          = "session.revoked"
	AuditAgentGrantCreated       = "agent_grant.created"
	AuditAgentGrantUpdated       = "agent_grant.updated"
	AuditAgentGrantRevoked       = "agent_grant.revoked"
)

type CredentialCreatedPayload struct {
	CredentialID string `json:"credential_id"`
}

type CredentialDeletedPayload struct {
	CredentialID string `json:"credential_id"`
}

type TokenMintedPayload struct {
	TokenID string `json:"token_id"`
}

type TokenRevokedPayload struct {
	TokenID string `json:"token_id"`
}

type AgentGrantPayload struct {
	GrantID string `json:"grant_id"`
}

type MeterCircuitPayload struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason"`
}

func CredentialCreatedEvent(credential Credential) AuditEvent {
	return typedAuditEvent(credential.UserID, "", AuditCredentialCreated, CredentialCreatedPayload{
		CredentialID: credential.ID,
	})
}

func CredentialDeletedEvent(userID, credentialID string) AuditEvent {
	return typedAuditEvent(userID, "", AuditCredentialDeleted, CredentialDeletedPayload{CredentialID: credentialID})
}

func TokenMintedEvent(token Token) AuditEvent {
	return typedAuditEvent(token.UserID, token.ID, AuditTokenMinted, TokenMintedPayload{
		TokenID: token.ID,
	})
}

func TokenEvent(token Token, eventType string) AuditEvent {
	return typedAuditEvent(token.UserID, token.ID, eventType, TokenMintedPayload{TokenID: token.ID})
}

func TokenRevokedEvent(userID, tokenID string) AuditEvent {
	return typedAuditEvent(userID, tokenID, AuditTokenRevoked, TokenRevokedPayload{TokenID: tokenID})
}

func AgentGrantEvent(grant AgentGrant, eventType string) AuditEvent {
	return typedAuditEvent(grant.UserID, "", eventType, AgentGrantPayload{GrantID: grant.ID})
}

func AuthTransactionEvent(eventType string, at time.Time) AuditEvent {
	event := typedAuditEvent("", "", eventType, struct{}{})
	event.CreatedAt = at.UTC()
	return event
}

func SessionEvent(userID, eventType string, at time.Time) AuditEvent {
	event := typedAuditEvent(userID, "", eventType, struct{}{})
	event.CreatedAt = at.UTC()
	return event
}

func MeterCircuitEvent(eventType string, payload MeterCircuitPayload, at time.Time) AuditEvent {
	event := typedAuditEvent("", "", eventType, payload)
	event.CreatedAt = at.UTC()
	return event
}

func typedAuditEvent(userID, tokenID, eventType string, payload any) AuditEvent {
	encoded, err := json.Marshal(payload)
	if err != nil {
		// Every payload accepted here is a closed struct containing only strings;
		// marshal failure is therefore a programming error, not request data.
		panic(err)
	}
	return AuditEvent{UserID: userID, TokenID: tokenID, EventType: eventType, Payload: encoded}
}
