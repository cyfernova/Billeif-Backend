package ap2

import (
	"fmt"
	"time"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
)

type VerifiableCredential struct {
	Context           []string               `json:"@context"`
	Type              []string               `json:"type"`
	ID                string                 `json:"id"`
	Issuer            string                 `json:"issuer"`
	IssuanceDate      string                 `json:"issuanceDate"`
	ExpirationDate    string                 `json:"expirationDate"`
	CredentialSubject map[string]interface{} `json:"credentialSubject"`
	Proof             CredentialProof        `json:"proof"`
}

type CredentialProof struct {
	Type               string `json:"type"`
	Created            string `json:"created"`
	ProofPurpose       string `json:"proofPurpose"`
	VerificationMethod string `json:"verificationMethod"`
	JWS                string `json:"jws"`
}

type CredentialSubject struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Capabilities []string               `json:"capabilities"`
	Attributes   map[string]interface{} `json:"attributes"`
}

type CredentialBuilder struct {
	issuer  string
	signer  *MandateSigner
	context []string
	types   []string
}

func NewCredentialBuilder(issuer string, signer *MandateSigner) *CredentialBuilder {
	return &CredentialBuilder{
		issuer:  issuer,
		signer:  signer,
		context: []string{"https://www.w3.org/2018/credentials/v1"},
		types:   []string{"VerifiableCredential"},
	}
}

func (cb *CredentialBuilder) WithContext(contexts []string) *CredentialBuilder {
	cb.context = append(cb.context, contexts...)
	return cb
}

func (cb *CredentialBuilder) WithType(types []string) *CredentialBuilder {
	cb.types = append(cb.types, types...)
	return cb
}

func (cb *CredentialBuilder) BuildCredential(subjectID string, subjectType string, capabilities []string, attributes map[string]interface{}, expiration time.Duration) (*VerifiableCredential, error) {
	now := time.Now().UTC()
	expirationDate := now.Add(expiration)

	credential := &VerifiableCredential{
		Context:        cb.context,
		Type:           cb.types,
		ID:             fmt.Sprintf("urn:uuid:%s", generateUUID()),
		Issuer:         cb.issuer,
		IssuanceDate:   now.Format(time.RFC3339),
		ExpirationDate: expirationDate.Format(time.RFC3339),
		CredentialSubject: map[string]interface{}{
			"id":           subjectID,
			"type":         subjectType,
			"capabilities": capabilities,
			"attributes":   attributes,
		},
	}

	proof, err := cb.createProof(credential)
	if err != nil {
		return nil, fmt.Errorf("failed to create proof: %w", err)
	}

	credential.Proof = proof
	return credential, nil
}

func (cb *CredentialBuilder) createProof(vc *VerifiableCredential) (CredentialProof, error) {
	now := time.Now().UTC()

	proofData := map[string]interface{}{
		"@context":          vc.Context,
		"type":              vc.Type,
		"id":                vc.ID,
		"issuer":            vc.Issuer,
		"issuanceDate":      vc.IssuanceDate,
		"expirationDate":    vc.ExpirationDate,
		"credentialSubject": vc.CredentialSubject,
	}

	jws, err := cb.signer.SignIntentMandate(proofData)
	if err != nil {
		return CredentialProof{}, err
	}

	return CredentialProof{
		Type:               "EcdsaSecp256k1Signature2019",
		Created:            now.Format(time.RFC3339),
		ProofPurpose:       "assertionMethod",
		VerificationMethod: fmt.Sprintf("%s#keys-1", cb.issuer),
		JWS:                jws,
	}, nil
}

func (cb *CredentialBuilder) BuildPaymentCredential(credentialID, userID string, credentialType string, attributes map[string]interface{}, expiration time.Duration) (*VerifiableCredential, error) {
	return cb.BuildCredential(
		fmt.Sprintf("did:example:%s", userID),
		"PaymentCredential",
		[]string{credentialType},
		attributes,
		expiration,
	)
}

func (cb *CredentialBuilder) BuildAgentCredential(agentID, agentName, agentType string, capabilities []string, expiration time.Duration) (*VerifiableCredential, error) {
	attributes := map[string]interface{}{
		"name": agentName,
		"type": agentType,
	}

	return cb.BuildCredential(
		fmt.Sprintf("did:example:%s", agentID),
		"AgentCredential",
		capabilities,
		attributes,
		expiration,
	)
}

type CredentialVerifier struct {
	verifier *MandateVerifier
}

func NewCredentialVerifier() *CredentialVerifier {
	return &CredentialVerifier{
		verifier: NewMandateVerifier(),
	}
}

func (cv *CredentialVerifier) VerifyCredential(vc *VerifiableCredential, publicKey string) error {
	expirationDate, err := time.Parse(time.RFC3339, vc.ExpirationDate)
	if err != nil {
		return fmt.Errorf("invalid expiration date: %w", err)
	}

	if time.Now().UTC().After(expirationDate) {
		return fmt.Errorf("credential has expired")
	}

	proofData := map[string]interface{}{
		"@context":          vc.Context,
		"type":              vc.Type,
		"id":                vc.ID,
		"issuer":            vc.Issuer,
		"issuanceDate":      vc.IssuanceDate,
		"expirationDate":    vc.ExpirationDate,
		"credentialSubject": vc.CredentialSubject,
	}

	return cv.verifier.VerifyIntentMandate(proofData, vc.Proof.JWS, publicKey)
}

func (cv *CredentialVerifier) VerifyPaymentCredential(vc *VerifiableCredential, publicKey string, requiredType string) error {
	if err := cv.VerifyCredential(vc, publicKey); err != nil {
		return err
	}

	subject, ok := vc.CredentialSubject["type"].(string)
	if !ok || subject != "PaymentCredential" {
		return fmt.Errorf("invalid credential type, expected PaymentCredential")
	}

	attributes, ok := vc.CredentialSubject["attributes"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid credential attributes")
	}

	credentialType, ok := attributes["type"].(string)
	if !ok || credentialType != requiredType {
		return fmt.Errorf("invalid payment credential type, expected %s", requiredType)
	}

	return nil
}

func (cv *CredentialVerifier) VerifyAgentCredential(vc *VerifiableCredential, publicKey string, requiredType string) error {
	if err := cv.VerifyCredential(vc, publicKey); err != nil {
		return err
	}

	subject, ok := vc.CredentialSubject["type"].(string)
	if !ok || subject != "AgentCredential" {
		return fmt.Errorf("invalid credential type, expected AgentCredential")
	}

	attributes, ok := vc.CredentialSubject["attributes"].(map[string]interface{})
	if !ok {
		return fmt.Errorf("invalid credential attributes")
	}

	agentType, ok := attributes["type"].(string)
	if !ok || agentType != requiredType {
		return fmt.Errorf("invalid agent type, expected %s", requiredType)
	}

	return nil
}

func ConvertCredentialToModel(vc *VerifiableCredential, userID string) (*models.PaymentCredential, error) {
	attributes, ok := vc.CredentialSubject["attributes"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid credential attributes")
	}

	credentialType, ok := attributes["type"].(string)
	if !ok {
		return nil, fmt.Errorf("missing credential type")
	}

	credential := &models.PaymentCredential{
		UserID:         userID,
		CredentialType: credentialType,
		IsActive:       true,
		IsDefault:      false,
	}

	if maskedCard, ok := attributes["masked_card"].(string); ok {
		credential.MaskedCardNumber = &maskedCard
	}

	if cardBrand, ok := attributes["card_brand"].(string); ok {
		credential.CardBrand = &cardBrand
	}

	if encryptedData, ok := attributes["encrypted_data"].(string); ok {
		credential.EncryptedData = &encryptedData
	}

	return credential, nil
}

func generateUUID() string {
	return uuid.New().String()
}
