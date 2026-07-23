package policy

import (
	"testing"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

func TestCheckAgentGrantRejectsBroaderOrExhaustedToken(t *testing.T) {
	perToken := int64(100)
	grantMax := int64(1000)
	grant := store.AgentGrant{
		Enabled: true, CredentialIDs: []string{"credential"}, AllowedModels: []string{"model"},
		PerTokenMaxTokens: &perToken, GrantMaxTokens: &grantMax,
	}
	valid := store.Token{CredentialIDs: []string{"credential"}, AllowedModels: []string{"model"}, MaxTotalTokens: &perToken}
	if err := CheckAgentGrant(grant, store.AgentGrantCounters{}, valid); err != nil {
		t.Fatalf("valid child token rejected: %v", err)
	}
	broader := valid
	broader.AllowedModels = []string{"model", "other"}
	if err := CheckAgentGrant(grant, store.AgentGrantCounters{}, broader); err == nil || err.Code != "token_revoked" {
		t.Fatalf("broader token result = %+v", err)
	}
	if err := CheckAgentGrant(grant, store.AgentGrantCounters{TotalTokens: grantMax}, valid); err == nil || err.Code != "budget_exhausted" {
		t.Fatalf("exhausted grant result = %+v", err)
	}
}
