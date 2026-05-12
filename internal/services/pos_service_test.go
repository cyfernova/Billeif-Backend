package services

import "testing"

func TestResolveTrustedPOSCheckoutCartRejectsClientLinesFallback(t *testing.T) {
	_, err := resolveTrustedPOSCheckoutCart(POSSessionCart{}, CheckoutPOSCartInput{
		Lines: []POSSessionCartLine{{
			ProductID: "product-1",
			Quantity:  1,
			UnitPrice: 1,
		}},
	})
	if err == nil {
		t.Fatal("expected client-supplied POS fallback lines to be rejected")
	}
}

func TestResolveTrustedPOSCheckoutCartUsesServerCart(t *testing.T) {
	cart, err := resolveTrustedPOSCheckoutCart(POSSessionCart{
		Items: []POSSessionCartLine{{
			ProductID: "product-1",
			Quantity:  2,
			UnitPrice: 150,
			TaxRate:   10,
		}},
	}, CheckoutPOSCartInput{})
	if err != nil {
		t.Fatalf("expected server cart to be accepted: %v", err)
	}
	if cart.Total != 330 {
		t.Fatalf("expected server cart total to be recalculated, got %.2f", cart.Total)
	}
}
