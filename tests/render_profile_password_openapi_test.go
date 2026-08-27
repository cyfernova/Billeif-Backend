package tests

import (
	"encoding/json"
	"testing"

	"invoice-backend/docs"
)

func TestRenderProfileOpenAPIExposesPasswordOnlyAsWriteOnlyInput(t *testing.T) {
	var document struct {
		Definitions map[string]struct {
			Properties map[string]map[string]interface{} `json:"properties"`
		} `json:"definitions"`
	}
	if err := json.Unmarshal([]byte(docs.SwaggerInfo.ReadDoc()), &document); err != nil {
		t.Fatalf("decode generated OpenAPI document: %v", err)
	}

	response := document.Definitions["models.RenderProfile"].Properties
	for _, forbidden := range []string{"password", "legacy_password", "password_ciphertext"} {
		if _, ok := response[forbidden]; ok {
			t.Fatalf("render profile response schema exposes %q", forbidden)
		}
	}
	if _, ok := response["password_configured"]; !ok {
		t.Fatal("render profile response schema omits password_configured")
	}

	for _, definition := range []string{
		"services.CreateRenderProfileInput",
		"services.UpdateRenderProfileInput",
	} {
		password := document.Definitions[definition].Properties["password"]
		writeOnly, ok := password["x-writeonly"].(bool)
		if !ok || !writeOnly {
			t.Fatalf("%s password property is not marked x-writeonly", definition)
		}
	}
}
