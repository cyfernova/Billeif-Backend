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
	GenerateEInvoice      bool                   `json:"generate_einvoice"`
	GenerateEWayBill      bool                   `json:"generate_ewaybill"`
	ReverseCharge         bool                   `json:"reverse_charge"`
	ReverseChargeReason   string                 `json:"reverse_charge_reason"`
	DispatchFrom          map[string]interface{} `json:"dispatch_from,omitempty"`
	DispatchTo            map[string]interface{} `json:"dispatch_to,omitempty"`
	DistanceKM            float64                `json:"distance_km,omitempty"`
	Transporter           map[string]interface{} `json:"transporter,omitempty"`
	Vehicle               map[string]interface{} `json:"vehicle,omitempty"`
	MultiVehiclePlan      map[string]interface{} `json:"multi_vehicle_plan,omitempty"`
	TCS                   []WithholdingInput     `json:"tcs,omitempty"`
	SourceLinkage         map[string]interface{} `json:"source_linkage,omitempty"`
	ReportTags            map[string]interface{} `json:"report_tags,omitempty"`
}
