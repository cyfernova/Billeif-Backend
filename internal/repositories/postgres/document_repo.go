package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"invoice-backend/internal/models"
	interfaces "invoice-backend/internal/repositories/interfaces"
)

type documentRepository struct {
	db *gorm.DB
}

func NewDocumentRepository(db *gorm.DB) interfaces.DocumentRepository {
	return &documentRepository{db: db}
}

func (r *documentRepository) Create(ctx context.Context, document *models.Document) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit(clause.Associations).Create(document).Error; err != nil {
			return err
		}
		if len(document.Lines) == 0 {
			return nil
		}
		for _, line := range document.Lines {
			line.DocumentID = document.ID
		}
		return tx.Create(&document.Lines).Error
	})
}

func (r *documentRepository) GetByID(ctx context.Context, id, businessID string) (*models.Document, error) {
	var document models.Document
	err := r.db.WithContext(ctx).
		Preload("Lines").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("document not found")
	}
	return &document, err
}

func (r *documentRepository) GetByIDInternal(ctx context.Context, id string) (*models.Document, error) {
	var document models.Document
	err := r.db.WithContext(ctx).
		Preload("Lines").
		Where("id = ? AND deleted_at IS NULL", id).
		First(&document).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("document not found")
	}
	return &document, err
}

func (r *documentRepository) ListByType(ctx context.Context, businessID, documentType string, page, limit int) ([]*models.Document, int64, error) {
	var documents []models.Document
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.Document{}).Where("business_id = ? AND deleted_at IS NULL", businessID)
	if documentType != "" {
		query = query.Where("document_type = ?", documentType)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("issue_date DESC, created_at DESC").Offset(offset).Limit(limit).Find(&documents).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.Document, len(documents))
	for i := range documents {
		result[i] = &documents[i]
	}
	return result, total, nil
}

func (r *documentRepository) Update(ctx context.Context, document *models.Document) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Document{}).
			Where("id = ?", document.ID).
			Updates(map[string]interface{}{
				"party_type":              document.PartyType,
				"party_id":                document.PartyID,
				"status":                  document.Status,
				"draft_state":             document.DraftState,
				"tax_mode":                document.TaxMode,
				"gst_treatment":           document.GSTTreatment,
				"place_of_supply":         document.PlaceOfSupply,
				"party_gstin":             document.PartyGSTIN,
				"party_pan":               document.PartyPAN,
				"party_state_code":        document.PartyStateCode,
				"supply_type":             document.SupplyType,
				"export_type":             document.ExportType,
				"bill_of_supply":          document.BillOfSupply,
				"serial_number":           document.SerialNumber,
				"issue_date":              document.IssueDate,
				"due_date":                document.DueDate,
				"dispatch_date":           document.DispatchDate,
				"currency":                document.Currency,
				"exchange_rate":           document.ExchangeRate,
				"locale":                  document.Locale,
				"source_linkage":          document.SourceLinkage,
				"render_profile_id":       document.RenderProfileID,
				"shipment_id":             document.ShipmentID,
				"project_id":              document.ProjectID,
				"price_list_id":           document.PriceListID,
				"origin_subscription_id":  document.OriginSubscriptionID,
				"origin_run_id":           document.OriginRunID,
				"profit_snapshot_enabled": document.ProfitSnapshotEnabled,
				"signed_at":               document.SignedAt,
				"signed_by_profile_id":    document.SignedByProfileID,
				"sign_metadata":           document.SignMetadata,
				"cancellation_reason":     document.CancellationReason,
				"cancelled_at":            document.CancelledAt,
				"pdf_url":                 document.PDFURL,
				"pdf_filename":            document.PDFFilename,
				"notes":                   document.Notes,
				"terms":                   document.Terms,
				"declaration":             document.Declaration,
				"direction":               document.Direction,
				"generate_einvoice":       document.GenerateEInvoice,
				"generate_ewaybill":       document.GenerateEWayBill,
				"reverse_charge":          document.ReverseCharge,
				"reverse_charge_reason":   document.ReverseChargeReason,
				"dispatch_from":           document.DispatchFrom,
				"dispatch_to":             document.DispatchTo,
				"distance_km":             document.DistanceKM,
				"transporter":             document.Transporter,
				"vehicle":                 document.Vehicle,
				"multi_vehicle_plan":      document.MultiVehiclePlan,
				"current_einvoice_id":     document.CurrentEInvoiceID,
				"current_ewaybill_id":     document.CurrentEWayBillID,
				"subtotal":                document.Subtotal,
				"discount_total":          document.DiscountTotal,
				"tax_total":               document.TaxTotal,
				"cess_total":              document.CessTotal,
				"withholding_total":       document.WithholdingTotal,
				"tds_total":               document.TDSTotal,
				"tcs_total":               document.TCSTotal,
				"total":                   document.Total,
				"paid_amount":             document.PaidAmount,
				"balance_due":             document.BalanceDue,
				"extra_fields":            document.ExtraFields,
				"report_tags":             document.ReportTags,
			}).Error; err != nil {
			return err
		}

		if err := tx.Where("document_id = ?", document.ID).Delete(&models.DocumentLine{}).Error; err != nil {
			return err
		}
		if len(document.Lines) == 0 {
			return nil
		}
		for _, line := range document.Lines {
			line.DocumentID = document.ID
		}
		return tx.Create(&document.Lines).Error
	})
}

func (r *documentRepository) UpdatePDF(ctx context.Context, documentID, pdfURL, filename string) error {
	return r.db.WithContext(ctx).Model(&models.Document{}).Where("id = ?", documentID).Updates(map[string]interface{}{
		"pdf_url":      pdfURL,
		"pdf_filename": filename,
	}).Error
}

func (r *documentRepository) Delete(ctx context.Context, id string) error {
	return r.db.WithContext(ctx).Where("id = ?", id).Delete(&models.Document{}).Error
}

func (r *documentRepository) CreateLink(ctx context.Context, link *models.DocumentLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *documentRepository) ListLinks(ctx context.Context, businessID, documentID string) ([]*models.DocumentLink, error) {
	var links []models.DocumentLink
	err := r.db.WithContext(ctx).
		Where("business_id = ? AND (source_document_id = ? OR target_document_id = ?)", businessID, documentID, documentID).
		Order("created_at DESC").
		Find(&links).Error
	if err != nil {
		return nil, err
	}
	result := make([]*models.DocumentLink, len(links))
	for i := range links {
		result[i] = &links[i]
	}
	return result, nil
}

func (r *documentRepository) CreateRenderProfile(ctx context.Context, profile *models.RenderProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if profile.IsDefault {
			if err := tx.Model(&models.RenderProfile{}).
				Where("business_id = ? AND deleted_at IS NULL", profile.BusinessID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		} else {
			var count int64
			if err := tx.Model(&models.RenderProfile{}).
				Where("business_id = ? AND is_default = TRUE AND deleted_at IS NULL", profile.BusinessID).
				Count(&count).Error; err != nil {
				return err
			}
			if count == 0 {
				profile.IsDefault = true
			}
		}
		return tx.Create(profile).Error
	})
}

func (r *documentRepository) ListRenderProfiles(ctx context.Context, businessID string, page, limit int) ([]*models.RenderProfile, int64, error) {
	var profiles []models.RenderProfile
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.RenderProfile{}).
		Where("business_id = ? AND deleted_at IS NULL", businessID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("is_default DESC, created_at DESC").Offset(offset).Limit(limit).Find(&profiles).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.RenderProfile, len(profiles))
	for i := range profiles {
		result[i] = &profiles[i]
	}
	return result, total, nil
}

func (r *documentRepository) GetRenderProfile(ctx context.Context, businessID, id string) (*models.RenderProfile, error) {
	var profile models.RenderProfile
	err := r.db.WithContext(ctx).Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("render profile not found")
	}
	return &profile, err
}

func (r *documentRepository) GetDefaultRenderProfile(ctx context.Context, businessID string) (*models.RenderProfile, error) {
	var profile models.RenderProfile
	err := r.db.WithContext(ctx).Where("business_id = ? AND is_default = TRUE AND deleted_at IS NULL", businessID).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("render profile not found")
	}
	return &profile, err
}

func (r *documentRepository) UpdateRenderProfile(ctx context.Context, profile *models.RenderProfile) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if profile.IsDefault {
			if err := tx.Model(&models.RenderProfile{}).
				Where("business_id = ? AND id <> ? AND deleted_at IS NULL", profile.BusinessID, profile.ID).
				Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Model(&models.RenderProfile{}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", profile.ID, profile.BusinessID).
			Updates(map[string]interface{}{
				"name":               profile.Name,
				"header_html":        profile.HeaderHTML,
				"footer_html":        profile.FooterHTML,
				"watermark_text":     profile.WatermarkText,
				"banner_text":        profile.BannerText,
				"font_family":        profile.FontFamily,
				"page_size":          profile.PageSize,
				"layout_config":      profile.LayoutConfig,
				"password_protected": profile.PasswordProtected,
				"password":           profile.Password,
				"copy_allowed":       profile.CopyAllowed,
				"print_allowed":      profile.PrintAllowed,
				"custom_labels":      profile.CustomLabels,
				"visibility_config":  profile.VisibilityConfig,
				"is_default":         profile.IsDefault,
			}).Error
	})
}

func (r *documentRepository) DeleteRenderProfile(ctx context.Context, businessID, id string) error {
	return r.db.WithContext(ctx).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		Delete(&models.RenderProfile{}).Error
}

func (r *documentRepository) CreateRenderJob(ctx context.Context, job *models.DocumentRenderJob) error {
	return r.db.WithContext(ctx).Create(job).Error
}

func (r *documentRepository) GetRenderJob(ctx context.Context, businessID, jobID string) (*models.DocumentRenderJob, error) {
	var job models.DocumentRenderJob
	query := r.db.WithContext(ctx).Where("id = ? AND deleted_at IS NULL", jobID)
	if businessID != "" {
		query = query.Where("business_id = ?", businessID)
	}
	err := query.First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("render job not found")
	}
	return &job, err
}

func (r *documentRepository) GetLatestRenderJob(ctx context.Context, documentID string) (*models.DocumentRenderJob, error) {
	var job models.DocumentRenderJob
	err := r.db.WithContext(ctx).
		Where("document_id = ? AND deleted_at IS NULL", documentID).
		Order("created_at DESC").
		First(&job).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("render job not found")
	}
	return &job, err
}

func (r *documentRepository) UpdateRenderJob(ctx context.Context, job *models.DocumentRenderJob) error {
	return r.db.WithContext(ctx).Save(job).Error
}

func (r *documentRepository) CreateRevision(ctx context.Context, revision *models.DocumentRevision) error {
	return r.db.WithContext(ctx).Create(revision).Error
}

func (r *documentRepository) ListRevisions(ctx context.Context, businessID, documentID string, page, limit int) ([]*models.DocumentRevision, int64, error) {
	var revisions []models.DocumentRevision
	var total int64

	offset := (page - 1) * limit
	query := r.db.WithContext(ctx).Model(&models.DocumentRevision{}).
		Where("business_id = ? AND document_id = ? AND deleted_at IS NULL", businessID, documentID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if err := query.Order("created_at DESC").Offset(offset).Limit(limit).Find(&revisions).Error; err != nil {
		return nil, 0, err
	}

	result := make([]*models.DocumentRevision, len(revisions))
	for i := range revisions {
		result[i] = &revisions[i]
	}
	return result, total, nil
}
