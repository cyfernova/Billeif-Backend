package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"
)

type permissionCheckCall struct {
	userID     string
	businessID string
	permission string
}

type recordingPermissionChecker struct {
	allow bool
	calls []permissionCheckCall
}

func (c *recordingPermissionChecker) UserHasPermission(_ context.Context, userID, businessID, permission string) bool {
	c.calls = append(c.calls, permissionCheckCall{userID: userID, businessID: businessID, permission: permission})
	return c.allow
}

type countingCustomerRepository struct {
	calls int
}

func (r *countingCustomerRepository) Create(context.Context, *models.Customer) error {
	r.calls++
	return nil
}
func (r *countingCustomerRepository) GetByID(context.Context, string, string) (*models.Customer, error) {
	r.calls++
	return &models.Customer{ID: "customer-1", BusinessID: "business-1"}, nil
}
func (r *countingCustomerRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.Customer, int64, error) {
	r.calls++
	return nil, 0, nil
}
func (r *countingCustomerRepository) Update(context.Context, *models.Customer) error {
	r.calls++
	return nil
}
func (r *countingCustomerRepository) Delete(context.Context, string) error {
	r.calls++
	return nil
}

type countingVendorRepository struct {
	calls int
}

func (r *countingVendorRepository) Create(context.Context, *models.Vendor) error {
	r.calls++
	return nil
}
func (r *countingVendorRepository) GetByID(context.Context, string, string) (*models.Vendor, error) {
	r.calls++
	return &models.Vendor{ID: "vendor-1", BusinessID: "business-1"}, nil
}
func (r *countingVendorRepository) GetByBusinessID(context.Context, string, int, int) ([]*models.Vendor, int64, error) {
	r.calls++
	return nil, 0, nil
}
func (r *countingVendorRepository) Update(context.Context, *models.Vendor) error {
	r.calls++
	return nil
}
func (r *countingVendorRepository) Delete(context.Context, string) error {
	r.calls++
	return nil
}

type countingRenderProfileRepository struct {
	interfaces.DocumentRepository
	calls int
}

func (r *countingRenderProfileRepository) CreateRenderProfile(context.Context, *models.RenderProfile) error {
	r.calls++
	return nil
}
func (r *countingRenderProfileRepository) GetRenderProfile(context.Context, string, string) (*models.RenderProfile, error) {
	r.calls++
	return &models.RenderProfile{ID: "profile-1", BusinessID: "business-1"}, nil
}
func (r *countingRenderProfileRepository) UpdateRenderProfile(context.Context, *models.RenderProfile) error {
	r.calls++
	return nil
}
func (r *countingRenderProfileRepository) DeleteRenderProfile(context.Context, string, string) error {
	r.calls++
	return nil
}

func mutationActorContext() context.Context {
	return ContextWithActor(context.Background(), ActorContext{UserID: "viewer-1", Role: "viewer"})
}

func assertPermissionDeniedBeforeRepository(t *testing.T, err error, repositoryCalls int, checker *recordingPermissionChecker, wantPermission string) {
	t.Helper()
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("mutation error = %v, want ErrPermissionDenied", err)
	}
	if repositoryCalls != 0 {
		t.Fatalf("repository calls = %d, want zero", repositoryCalls)
	}
	wantCall := permissionCheckCall{userID: "viewer-1", businessID: "business-1", permission: wantPermission}
	if len(checker.calls) != 1 || checker.calls[0] != wantCall {
		t.Fatalf("permission calls = %#v, want %#v", checker.calls, []permissionCheckCall{wantCall})
	}
}

func TestCustomerServiceDeniesUnauthorizedMutationsBeforeRepository(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		mutate     func(*CustomerService) error
	}{
		{name: "create", permission: PermissionCustomersCreate, mutate: func(service *CustomerService) error {
			_, err := service.Create(mutationActorContext(), CreateCustomerInput{BusinessID: "business-1", Name: "Customer", Email: "customer@example.com"})
			return err
		}},
		{name: "update", permission: PermissionCustomersUpdate, mutate: func(service *CustomerService) error {
			_, err := service.UpdateByBusiness(mutationActorContext(), "business-1", "customer-1", UpdateCustomerInput{Name: "Updated"})
			return err
		}},
		{name: "delete", permission: PermissionCustomersDelete, mutate: func(service *CustomerService) error {
			return service.DeleteByBusiness(mutationActorContext(), "business-1", "customer-1")
		}},
		{name: "direct import", permission: PermissionCustomersCreate, mutate: func(service *CustomerService) error {
			_, err := service.Import(mutationActorContext(), "business-1", []CreateCustomerInput{{Name: "Customer", Email: "customer@example.com"}})
			return err
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &countingCustomerRepository{}
			checker := &recordingPermissionChecker{}
			service := NewCustomerService(repository, checker, logger.New()).WithCapabilityGuard(&recordingCapabilityGuard{})
			err := test.mutate(service)
			assertPermissionDeniedBeforeRepository(t, err, repository.calls, checker, test.permission)
		})
	}
}

func TestVendorServiceDeniesUnauthorizedMutationsBeforeRepository(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		mutate     func(*VendorService) error
	}{
		{name: "create", permission: PermissionVendorsCreate, mutate: func(service *VendorService) error {
			_, err := service.Create(mutationActorContext(), CreateVendorInput{BusinessID: "business-1", Name: "Vendor", Email: "vendor@example.com"})
			return err
		}},
		{name: "update", permission: PermissionVendorsUpdate, mutate: func(service *VendorService) error {
			_, err := service.UpdateByBusiness(mutationActorContext(), "business-1", "vendor-1", UpdateVendorInput{Name: "Updated"})
			return err
		}},
		{name: "delete", permission: PermissionVendorsDelete, mutate: func(service *VendorService) error {
			return service.DeleteByBusiness(mutationActorContext(), "business-1", "vendor-1")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &countingVendorRepository{}
			checker := &recordingPermissionChecker{}
			service := NewVendorService(repository, checker, logger.New())
			err := test.mutate(service)
			assertPermissionDeniedBeforeRepository(t, err, repository.calls, checker, test.permission)
		})
	}
}

func TestRenderProfileServiceDeniesUnauthorizedMutationsBeforeRepository(t *testing.T) {
	tests := []struct {
		name       string
		permission string
		mutate     func(*DocumentService) error
	}{
		{name: "create", permission: PermissionRenderProfilesCreate, mutate: func(service *DocumentService) error {
			_, err := service.CreateRenderProfileByBusiness(mutationActorContext(), "business-1", CreateRenderProfileInput{Name: "Profile"})
			return err
		}},
		{name: "update", permission: PermissionRenderProfilesUpdate, mutate: func(service *DocumentService) error {
			_, err := service.UpdateRenderProfileByBusiness(mutationActorContext(), "business-1", "profile-1", UpdateRenderProfileInput{Name: "Updated"})
			return err
		}},
		{name: "delete", permission: PermissionRenderProfilesDelete, mutate: func(service *DocumentService) error {
			return service.DeleteRenderProfileByBusiness(mutationActorContext(), "business-1", "profile-1")
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &countingRenderProfileRepository{}
			checker := &recordingPermissionChecker{}
			service := NewDocumentService(nil, nil, nil, repository, nil, nil, nil, nil, nil, nil, nil, &awsclients.Config{}, checker, logger.New())
			err := test.mutate(service)
			assertPermissionDeniedBeforeRepository(t, err, repository.calls, checker, test.permission)
		})
	}
}

func TestQueuedPartyImportsDenyUnauthorizedActorBeforeDatabase(t *testing.T) {
	tests := []struct {
		name       string
		jobType    string
		permission string
	}{
		{name: "customers", jobType: models.BulkJobTypeImportCustomers, permission: PermissionCustomersCreate},
		{name: "vendors", jobType: models.BulkJobTypeImportVendors, permission: PermissionVendorsCreate},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			checker := &recordingPermissionChecker{}
			service := NewBillingOpsService(nil, nil, nil, nil, nil, nil, nil, nil, checker, logger.New()).WithCapabilityGuard(&recordingCapabilityGuard{})
			_, err := service.CreateBulkJob(mutationActorContext(), CreateBulkJobInput{BusinessID: "business-1", JobType: test.jobType})
			assertPermissionDeniedBeforeRepository(t, err, 0, checker, test.permission)
		})
	}
}

func TestBulkImportIntakeRejectsUnsupportedCapabilityBeforeAnyEffect(t *testing.T) {
	unavailable := &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityBulkImports,
		State: CapabilityStateUnsupported, ReasonCode: ReasonBulkProcessorUnavailable,
	}

	t.Run("direct customer import", func(t *testing.T) {
		repository := &countingCustomerRepository{}
		checker := &recordingPermissionChecker{allow: true}
		guard := &recordingCapabilityGuard{err: unavailable}
		service := NewCustomerService(repository, checker, logger.New()).WithCapabilityGuard(guard)

		_, err := service.Import(mutationActorContext(), "business-1", []CreateCustomerInput{{Name: "Customer", Email: "customer@example.com"}})

		var capabilityErr *CapabilityUnavailableError
		if !errors.As(err, &capabilityErr) {
			t.Fatalf("import error = %T %v, want CapabilityUnavailableError", err, err)
		}
		if repository.calls != 0 || len(checker.calls) != 0 {
			t.Fatalf("repository calls = %d permission calls = %d, want zero effects", repository.calls, len(checker.calls))
		}
	})

	t.Run("queued import", func(t *testing.T) {
		checker := &recordingPermissionChecker{allow: true}
		guard := &recordingCapabilityGuard{err: unavailable}
		service := NewBillingOpsService(nil, nil, nil, nil, nil, nil, nil, nil, checker, logger.New()).WithCapabilityGuard(guard)

		_, err := service.CreateBulkJob(mutationActorContext(), CreateBulkJobInput{
			BusinessID: "business-1", JobType: models.BulkJobTypeImportProducts, FileContent: []byte("unsafe"),
		})

		var capabilityErr *CapabilityUnavailableError
		if !errors.As(err, &capabilityErr) {
			t.Fatalf("import error = %T %v, want CapabilityUnavailableError", err, err)
		}
		if len(checker.calls) != 0 {
			t.Fatalf("permission calls = %d, want zero effects", len(checker.calls))
		}
	})
}

func TestLegacyBulkImportIntakeRemainsDisabledAfterCapabilityEnablement(t *testing.T) {
	service := NewBillingOpsService(nil, nil, nil, nil, nil, nil, nil, nil, &recordingPermissionChecker{allow: true}, logger.New()).WithCapabilityGuard(&recordingCapabilityGuard{})

	_, err := service.CreateBulkJob(mutationActorContext(), CreateBulkJobInput{
		BusinessID: "business-1", JobType: models.BulkJobTypeImportCustomers, FileContent: []byte("unsafe"),
	})

	if !errors.Is(err, ErrLegacyBulkImportDisabled) {
		t.Fatalf("legacy import error = %v, want ErrLegacyBulkImportDisabled", err)
	}
}

func TestMutationAuthorizationFailsClosedWithoutCheckerOrServerActor(t *testing.T) {
	tests := []struct {
		name    string
		ctx     context.Context
		checker PermissionChecker
	}{
		{name: "missing checker", ctx: mutationActorContext(), checker: nil},
		{name: "missing server actor", ctx: context.Background(), checker: &recordingPermissionChecker{allow: true}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &countingCustomerRepository{}
			service := NewCustomerService(repository, test.checker, logger.New())
			_, err := service.Create(test.ctx, CreateCustomerInput{BusinessID: "business-1", Name: "Customer", Email: "customer@example.com"})
			if !errors.Is(err, ErrPermissionDenied) {
				t.Fatalf("mutation error = %v, want ErrPermissionDenied", err)
			}
			if repository.calls != 0 {
				t.Fatalf("repository calls = %d, want zero", repository.calls)
			}
		})
	}
}
