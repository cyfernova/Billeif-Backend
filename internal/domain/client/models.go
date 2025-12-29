package client

import (
	"time"

	"github.com/google/uuid"
)

type Address struct {
	Street  string `json:"street"`
	City    string `json:"city"`
	State   string `json:"state"`
	ZipCode string `json:"zip_code"`
	Country string `json:"country"`
}

type Client struct {
	ID        string    `json:"id"`
	Name      string    `json:"name" binding:"required"`
	Email     string    `json:"email" binding:"required,email"`
	Phone     string    `json:"phone"`
	Address   Address   `json:"address"`
	TaxID     string    `json:"tax_id"`
	LogoS3Key string    `json:"logo_s3_key,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type CreateClientRequest struct {
	Name    string  `json:"name" binding:"required,min=2"`
	Email   string  `json:"email" binding:"required,email"`
	Phone   string  `json:"phone"`
	Address Address `json:"address"`
	TaxID   string  `json:"tax_id"`
}

type UpdateClientRequest struct {
	Name    *string  `json:"name" binding:"omitempty,min=2"`
	Email   *string  `json:"email" binding:"omitempty,email"`
	Phone   *string  `json:"phone"`
	Address *Address `json:"address"`
	TaxID   *string  `json:"tax_id"`
}

type UploadLogoRequest struct {
	File string `json:"file"`
}

func NewClient(name, email, phone, taxID string, address Address) *Client {
	now := time.Now()
	return &Client{
		ID:        uuid.New().String(),
		Name:      name,
		Email:     email,
		Phone:     phone,
		Address:   address,
		TaxID:     taxID,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func (c *Client) Update(name, email, phone, taxID string, address Address) {
	if name != "" {
		c.Name = name
	}
	if email != "" {
		c.Email = email
	}
	if phone != "" {
		c.Phone = phone
	}
	if taxID != "" {
		c.TaxID = taxID
	}
	if address.Street != "" || address.City != "" || address.State != "" {
		c.Address = address
	}
	c.UpdatedAt = time.Now()
}
