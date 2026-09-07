package reporting

import "time"

type Column struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

type Definition struct {
	Key            string   `json:"key"`
	Name           string   `json:"name"`
	Category       string   `json:"category"`
	Description    string   `json:"description"`
	Family         string   `json:"family"`
	GroupBy        string   `json:"group_by,omitempty"`
	DocumentTypes  []string `json:"document_types,omitempty"`
	DefaultColumns []Column `json:"default_columns"`
}

type Filters struct {
	DateFrom                 *time.Time `json:"date_from,omitempty"`
	DateTo                   *time.Time `json:"date_to,omitempty"`
	CompareFrom              *time.Time `json:"compare_from,omitempty"`
	CompareTo                *time.Time `json:"compare_to,omitempty"`
	BranchID                 string     `json:"branch_id,omitempty"`
	Currency                 string     `json:"currency,omitempty"`
	AccountCode              string     `json:"account_code,omitempty"`
	ProjectID                string     `json:"project_id,omitempty"`
	WarehouseID              string     `json:"warehouse_id,omitempty"`
	PartyID                  string     `json:"party_id,omitempty"`
	ProductID                string     `json:"product_id,omitempty"`
	VariantID                string     `json:"variant_id,omitempty"`
	CategoryID               string     `json:"category_id,omitempty"`
	Search                   string     `json:"search,omitempty"`
	IncludeCancelled         bool       `json:"include_cancelled,omitempty"`
	AllowedBranchIDs         []string   `json:"-"`
	AllowedWarehouseIDs      []string   `json:"-"`
	BranchScopeRestricted    bool       `json:"-"`
	WarehouseScopeRestricted bool       `json:"-"`
}

type Query struct {
	BusinessID string   `json:"-"`
	UserID     string   `json:"-"`
	Page       int      `json:"page,omitempty"`
	Limit      int      `json:"limit,omitempty"`
	Columns    []string `json:"columns,omitempty"`
	Filters    Filters  `json:"filters,omitempty"`
}

type Pagination struct {
	Page  int   `json:"page"`
	Limit int   `json:"limit"`
	Total int64 `json:"total"`
}

type Result struct {
	Columns      []Column                 `json:"columns"`
	Rows         []map[string]interface{} `json:"rows"`
	Totals       map[string]interface{}   `json:"totals"`
	Pagination   Pagination               `json:"pagination"`
	ClipboardTSV string                   `json:"clipboard_tsv"`
}
