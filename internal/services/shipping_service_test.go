package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type shippingRepositoryFake struct {
	interfaces.ShippingRepository
	shipment *models.Shipment
	err      error
	updates  int
}

func (r *shippingRepositoryFake) GetShipmentByDocument(context.Context, string, string) (*models.Shipment, error) {
	return r.shipment, r.err
}
func (r *shippingRepositoryFake) UpdateShipment(context.Context, *models.Shipment) error {
	r.updates++
	return nil
}
func (r *shippingRepositoryFake) GetShippingLabelByDocument(context.Context, string, string) (*models.ShippingLabel, error) {
	return nil, errors.New("label not found")
}
func (r *shippingRepositoryFake) CreateShippingLabel(context.Context, *models.ShippingLabel) error {
	return nil
}

func TestShippingLabelPreservesSavedRecipientAndPackage(t *testing.T) {
	shipment := &models.Shipment{ID: "shipment", BusinessID: "business", DocumentID: "document", Provider: "manual", Courier: "Test courier", PackageCount: 3, WeightKG: 2.5, AddressSnapshot: `{"name":"Chosen Recipient","address":"12 Test Street"}`, PackageDimensions: `{"length":20}`, ProviderPayload: `{"estimated_delivery":"2026-09-12"}`}
	repo := &shippingRepositoryFake{shipment: shipment}
	service := &ShippingService{repo: repo}
	party := "vendor"
	document := &models.Document{ID: "document", BusinessID: "business", PartyID: &party, PartyType: models.DocumentPartyTypeVendor, SerialNumber: "PI-TEST", ExtraFields: `{"package_count":1,"address_snapshot":{"name":"Default Vendor"}}`}
	result, err := service.CreateLabelForDocument(context.Background(), document, ShippingRequestInput{})
	if err != nil {
		t.Fatal(err)
	}
	if stringValue(unmarshalJSONMap(result.Shipment.AddressSnapshot)["name"]) != "Chosen Recipient" || result.Shipment.PackageCount != 3 || result.Shipment.WeightKG != 2.5 || floatValue(unmarshalJSONMap(result.Shipment.PackageDimensions)["length"]) != 20 || stringValue(unmarshalJSONMap(result.Shipment.ProviderPayload)["estimated_delivery"]) != "2026-09-12" {
		t.Fatalf("saved shipment details were overwritten: %+v", result.Shipment)
	}
	if result.Label.LabelURL != "manual://shipping-label/document" {
		t.Fatalf("unexpected label: %+v", result.Label)
	}
}

func TestShippingExplicitRecipientUpdateWins(t *testing.T) {
	repo := &shippingRepositoryFake{shipment: &models.Shipment{ID: "shipment", Provider: "manual", AddressSnapshot: `{"name":"Old"}`}}
	service := &ShippingService{repo: repo}
	got, err := service.CreateOrUpdateShipmentForDocument(context.Background(), &models.Document{ID: "document", BusinessID: "business"}, ShippingRequestInput{AddressSnapshot: map[string]interface{}{"name": "New"}, PackageCount: 2})
	if err != nil || stringValue(unmarshalJSONMap(got.AddressSnapshot)["name"]) != "New" || got.PackageCount != 2 {
		t.Fatalf("explicit update failed: %v", err)
	}
}

func TestShippingReadFailureDoesNotWrite(t *testing.T) {
	want := errors.New("database unavailable")
	repo := &shippingRepositoryFake{err: want}
	service := &ShippingService{repo: repo}
	_, err := service.CreateOrUpdateShipmentForDocument(context.Background(), &models.Document{ID: "document", BusinessID: "business"}, ShippingRequestInput{})
	if !errors.Is(err, want) || repo.updates != 0 {
		t.Fatalf("database failure swallowed: %v", err)
	}
}
