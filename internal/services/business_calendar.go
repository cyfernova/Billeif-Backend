package services

import (
	"context"
	"fmt"
	"time"

	"invoice-backend/internal/models"
)

type businessTimezoneProvider interface {
	GetByID(context.Context, string) (*models.BusinessProfile, error)
}

func businessCalendarLocation(ctx context.Context, provider businessTimezoneProvider, businessID string) (*time.Location, error) {
	if provider == nil {
		return time.UTC, nil
	}
	business, err := provider.GetByID(ctx, businessID)
	if err != nil {
		return nil, fmt.Errorf("load business timezone: %w", err)
	}
	if business == nil || business.Timezone == "" {
		return nil, fmt.Errorf("business timezone unavailable")
	}
	location, err := time.LoadLocation(business.Timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid business timezone: %w", err)
	}
	return location, nil
}
