package services

type WithholdingInput struct {
	SectionCode     string                 `json:"section_code"`
	WithholdingType string                 `json:"withholding_type"`
	Rate            float64                `json:"rate"`
	TaxableAmount   float64                `json:"taxable_amount"`
	Amount          float64                `json:"amount"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type TaxProfileInput struct {
	GSTTreatment          string                 `json:"gst_treatment"`
	PlaceOfSupply         string                 `json:"place_of_supply"`
	BillOfSupply          bool                   `json:"bill_of_supply"`
	ExportType            string                 `json:"export_type"`
	SupplyType            string                 `json:"supply_type"`
	CounterpartyGSTIN     string                 `json:"counterparty_gstin"`
	CounterpartyPAN       string                 `json:"counterparty_pan"`
	CounterpartyStateCode string                 `json:"counterparty_state_code"`
	TCS                   []WithholdingInput     `json:"tcs,omitempty"`
	SourceLinkage         map[string]interface{} `json:"source_linkage,omitempty"`
	ReportTags            map[string]interface{} `json:"report_tags,omitempty"`
}
